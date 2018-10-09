package pbft

import (
	"encoding/base64"
	"fmt"
	"github.com/DSiSc/craft/log"
	"github.com/DSiSc/spree/consensus"
	"github.com/DSiSc/spree/pbft/common"
	"github.com/DSiSc/spree/pbft/tools"
	"github.com/golang/protobuf/proto"
	"github.com/spf13/viper"
	"time"
)

type pbftCore struct {
	consumer innerStack

	// pbft context
	activeView    bool              // view change happening
	byzantine     bool              // whether this node is intentionally acting as Byzantine; useful for debugging on the testnet
	f             int               // max. number of faults we can tolerate
	N             int               // max.number of validators in the network
	h             uint64            // low watermark
	id            uint64            // replica ID; PBFT `i`
	K             uint64            // checkpoint period
	logMultiplier uint64            // use this value to calculate log size : k*logMultiplier
	L             uint64            // log size
	lastExec      uint64            // last request we executed
	replicaCount  int               // number of replicas; PBFT `|R|`
	seqNo         uint64            // PBFT "n", strictly monotonic increasing sequence number
	view          uint64            // current view
	chkpts        map[uint64]string // state checkpoints; map lastExec to global hash
	pset          map[uint64]*common.ViewChange_PQ
	qset          map[qidx]*common.ViewChange_PQ

	skipInProgress    bool               // Set when we have detected a fall behind scenario until we pick a new starting point
	stateTransferring bool               // Set when state transfer is executing
	highStateTarget   *stateUpdateTarget // Set to the highest weak checkpoint cert we have observed
	hChkpts           map[uint64]uint64  // highest checkpoint sequence number observed for each replica

	currentExec           *uint64                         // currently executing request
	timerActive           bool                            // is the timer running?
	vcResendTimer         tools.Timer                     // timer triggering resend of a view change
	newViewTimer          tools.Timer                     // timeout triggering a view change
	requestTimeout        time.Duration                   // progress timeout for requests
	vcResendTimeout       time.Duration                   // timeout before resending view change
	newViewTimeout        time.Duration                   // progress timeout for new views
	newViewTimerReason    string                          // what triggered the timer
	lastNewViewTimeout    time.Duration                   // last timeout we used during this view change
	outstandingReqBatches map[string]*common.RequestBatch // track whether we are waiting for request batches to execute

	nullRequestTimer   tools.Timer   // timeout triggering a null request
	nullRequestTimeout time.Duration // duration for this timeout
	viewChangePeriod   uint64        // period between automatic view changes
	viewChangeSeqNo    uint64        // next seqNo to perform view change

	missingReqBatches map[string]bool // for all the assigned, non-checkpointed request batches we might be missing during view-change

	// implementation of PBFT `in`
	reqBatchStore   map[string]*common.RequestBatch // track request batches
	certStore       map[msgID]*msgCert              // track quorum certificates for requests
	checkpointStore map[common.Checkpoint]bool      // track checkpoints as set
	viewChangeStore map[vcidx]*common.ViewChange    // track view-change messages
	newViewStore    map[uint64]*common.NewView      // track last new-view we received or sent
}

type qidx struct {
	d string
	n uint64
}

type msgID struct { // our index through certStore
	v uint64
	n uint64
}

type vcidx struct {
	v  uint64
	id uint64
}

type msgCert struct {
	digest      string
	prePrepare  *common.PrePrepare
	sentPrepare bool
	prepare     []*common.Prepare
	sentCommit  bool
	commit      []*common.Commit
}

type checkpointMessage struct {
	seqNo uint64
	id    []byte
}

type stateUpdateTarget struct {
	checkpointMessage
	replicas []uint64
}

// Unless otherwise noted, all methods consume the PBFT thread, and should therefore
// not rely on PBFT accomplishing any work while that thread is being held
type innerStack interface {
	broadcast(msgPayload []byte)
	unicast(msgPayload []byte, receiverID uint64) (err error)
	execute(seqNo uint64, reqBatch *common.RequestBatch) // This is invoked on a separate thread
	getState() []byte
	getLastSeqNo() (uint64, error)
	skipTo(seqNo uint64, snapshotID []byte, peers []uint64)

	sign(msg []byte) ([]byte, error)
	verify(senderID uint64, signature []byte, message []byte) error

	invalidateState()
	validateState()

	consensus.StatePersistor
}

func newPbftCore(id uint64, config *viper.Viper, consumer innerStack, etf tools.TimerFactory) *pbftCore {
	var err error
	instance := &pbftCore{}
	instance.id = id
	instance.consumer = consumer

	instance.newViewTimer = etf.CreateTimer()
	instance.vcResendTimer = etf.CreateTimer()
	instance.nullRequestTimer = etf.CreateTimer()

	// check N and f
	instance.N = config.GetInt("general.N")
	instance.f = config.GetInt("general.f")
	if instance.f*3+1 > instance.N {
		panic(fmt.Sprintf("need at least %d enough replicas to tolerate %d byzantine faults, but only %d replicas configured", instance.f*3+1, instance.f, instance.N))
	}
	// checkpoint period
	instance.K = uint64(config.GetInt("general.K"))
	// use this value to calculate log size : k*logMultiplier
	instance.logMultiplier = uint64(config.GetInt("general.logmultiplier"))
	if instance.logMultiplier < 2 {
		panic("Log multiplier must be greater than or equal to 2")
	}
	instance.L = instance.logMultiplier * instance.K // log size
	instance.viewChangePeriod = uint64(config.GetInt("general.viewchangeperiod"))

	instance.byzantine = config.GetBool("general.byzantine")

	instance.requestTimeout, err = time.ParseDuration(config.GetString("general.timeout.request"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse request timeout: %s", err))
	}
	instance.vcResendTimeout, err = time.ParseDuration(config.GetString("general.timeout.resendviewchange"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse request timeout: %s", err))
	}
	instance.newViewTimeout, err = time.ParseDuration(config.GetString("general.timeout.viewchange"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse new view timeout: %s", err))
	}
	instance.nullRequestTimeout, err = time.ParseDuration(config.GetString("general.timeout.nullrequest"))
	if err != nil {
		instance.nullRequestTimeout = 0
	}

	instance.activeView = true
	instance.replicaCount = instance.N

	log.Info("PBFT type = %T", instance.consumer)
	log.Info("PBFT Max number of validating peers (N) = %v", instance.N)
	log.Info("PBFT Max number of failing peers (f) = %v", instance.f)
	log.Info("PBFT byzantine flag = %v", instance.byzantine)
	log.Info("PBFT request timeout = %v", instance.requestTimeout)
	log.Info("PBFT view change timeout = %v", instance.newViewTimeout)
	log.Info("PBFT Checkpoint period (K) = %v", instance.K)
	log.Info("PBFT Log multiplier = %v", instance.logMultiplier)
	log.Info("PBFT log size (L) = %v", instance.L)
	if instance.nullRequestTimeout > 0 {
		log.Info("PBFT null requests timeout = %v", instance.nullRequestTimeout)
	} else {
		log.Info("PBFT null requests disabled")
	}
	if instance.viewChangePeriod > 0 {
		log.Info("PBFT view change period = %v", instance.viewChangePeriod)
	} else {
		log.Info("PBFT automatic view change disabled")
	}

	// init the logs
	instance.certStore = make(map[msgID]*msgCert)
	instance.reqBatchStore = make(map[string]*common.RequestBatch)
	instance.checkpointStore = make(map[common.Checkpoint]bool)
	instance.chkpts = make(map[uint64]string)
	instance.viewChangeStore = make(map[vcidx]*common.ViewChange)
	instance.pset = make(map[uint64]*common.ViewChange_PQ)
	instance.qset = make(map[qidx]*common.ViewChange_PQ)
	instance.newViewStore = make(map[uint64]*common.NewView)

	// initialize state transfer
	instance.hChkpts = make(map[uint64]uint64)

	instance.chkpts[0] = "XXX GENESIS"

	instance.lastNewViewTimeout = instance.newViewTimeout
	instance.outstandingReqBatches = make(map[string]*common.RequestBatch)
	instance.missingReqBatches = make(map[string]bool)

	instance.restoreState()

	instance.viewChangeSeqNo = ^uint64(0) // infinity
	instance.updateViewChangeSeqNo()

	return instance
}

func (instance *pbftCore) restoreState() {
	updateSeqView := func(set []*common.ViewChange_PQ) {
		for _, e := range set {
			if instance.view < e.View {
				instance.view = e.View
			}
			if instance.seqNo < e.SequenceNumber {
				instance.seqNo = e.SequenceNumber
			}
		}
	}

	set := instance.restorePQSet("pset")
	for _, e := range set {
		instance.pset[e.SequenceNumber] = e
	}
	updateSeqView(set)

	set = instance.restorePQSet("qset")
	for _, e := range set {
		instance.qset[qidx{e.BatchDigest, e.SequenceNumber}] = e
	}
	updateSeqView(set)

	reqBatchesPacked, err := instance.consumer.ReadStateSet("reqBatch.")
	if err == nil {
		for k, v := range reqBatchesPacked {
			reqBatch := &common.RequestBatch{}
			err = proto.Unmarshal(v, reqBatch)
			if err != nil {
				log.Warn("Replica %d could not restore request batch %s", instance.id, k)
			} else {
				instance.reqBatchStore[tools.Hash(reqBatch)] = reqBatch
			}
		}
	} else {
		log.Warn("Replica %d could not restore reqBatchStore: %s", instance.id, err)
	}

	chkpts, err := instance.consumer.ReadStateSet("chkpt.")
	if err == nil {
		highSeq := uint64(0)
		for key, id := range chkpts {
			var seqNo uint64
			if _, err = fmt.Sscanf(key, "chkpt.%d", &seqNo); err != nil {
				log.Warn("Replica %d could not restore checkpoint key %s", instance.id, key)
			} else {
				idAsString := base64.StdEncoding.EncodeToString(id)
				log.Debug("Replica %d found checkpoint %s for seqNo %d", instance.id, idAsString, seqNo)
				instance.chkpts[seqNo] = idAsString
				if seqNo > highSeq {
					highSeq = seqNo
				}
			}
		}
		instance.moveWatermarks(highSeq)
	} else {
		log.Warn("Replica %d could not restore checkpoints: %s", instance.id, err)
	}

	instance.restoreLastSeqNo()

	log.Info("Replica %d restored state: view: %d, seqNo: %d, pset: %d, qset: %d, reqBatches: %d, chkpts: %d",
		instance.id, instance.view, instance.seqNo, len(instance.pset), len(instance.qset), len(instance.reqBatchStore), len(instance.chkpts))
}

func (instance *pbftCore) restoreLastSeqNo() {
	var err error
	if instance.lastExec, err = instance.consumer.getLastSeqNo(); err != nil {
		log.Warn("Replica %d could not restore lastExec: %s", instance.id, err)
		instance.lastExec = 0
	}
	log.Info("Replica %d restored lastExec: %d", instance.id, instance.lastExec)
}

func (instance *pbftCore) moveWatermarks(n uint64) {
	// round down n to previous low watermark
	h := n / instance.K * instance.K

	for idx, cert := range instance.certStore {
		if idx.n <= h {
			log.Debug("Replica %d cleaning quorum certificate for view=%d/seqNo=%d",
				instance.id, idx.v, idx.n)
			instance.persistDelRequestBatch(cert.digest)
			delete(instance.reqBatchStore, cert.digest)
			delete(instance.certStore, idx)
		}
	}

	for testChkpt := range instance.checkpointStore {
		if testChkpt.SequenceNumber <= h {
			log.Debug("Replica %d cleaning checkpoint message from replica %d, seqNo %d, b64 snapshot id %s",
				instance.id, testChkpt.ReplicaId, testChkpt.SequenceNumber, testChkpt.Id)
			delete(instance.checkpointStore, testChkpt)
		}
	}

	for n := range instance.pset {
		if n <= h {
			delete(instance.pset, n)
		}
	}

	for idx := range instance.qset {
		if idx.n <= h {
			delete(instance.qset, idx)
		}
	}

	for n := range instance.chkpts {
		if n < h {
			delete(instance.chkpts, n)
			instance.persistDelCheckpoint(n)
		}
	}

	instance.h = h

	log.Debug("Replica %d updated low watermark to %d",
		instance.id, instance.h)

	instance.resubmitRequestBatches()
}

func (instance *pbftCore) persistDelCheckpoint(seqNo uint64) {
	key := fmt.Sprintf("chkpt.%d", seqNo)
	instance.consumer.DelState(key)
}

func (instance *pbftCore) persistDelRequestBatch(digest string) {
	instance.consumer.DelState("reqBatch." + digest)
}

func (instance *pbftCore) resubmitRequestBatches() {
	if instance.primary(instance.view) != instance.id {
		return
	}

	var submissionOrder []*common.RequestBatch

outer:
	for d, reqBatch := range instance.outstandingReqBatches {
		for _, cert := range instance.certStore {
			if cert.digest == d {
				log.Debug("Replica %d already has certificate for request batch %s - not going to resubmit", instance.id, d)
				continue outer
			}
		}
		log.Debug("Replica %d has detected request batch %s must be resubmitted", instance.id, d)
		submissionOrder = append(submissionOrder, reqBatch)
	}

	if len(submissionOrder) == 0 {
		return
	}

	for _, reqBatch := range submissionOrder {
		// This is a request batch that has not been pre-prepared yet
		// Trigger request batch processing again
		instance.recvRequestBatch(reqBatch)
	}
}

func (instance *pbftCore) recvRequestBatch(reqBatch *common.RequestBatch) error {
	digest := tools.Hash(reqBatch)
	log.Debug("Replica %d received request batch %s", instance.id, digest)

	instance.reqBatchStore[digest] = reqBatch
	instance.outstandingReqBatches[digest] = reqBatch
	instance.persistRequestBatch(digest)
	if instance.activeView {
		instance.softStartTimer(instance.requestTimeout, fmt.Sprintf("new request batch %s", digest))
	}
	if instance.primary(instance.view) == instance.id && instance.activeView {
		instance.nullRequestTimer.Stop()
		instance.sendPrePrepare(reqBatch, digest)
	} else {
		log.Debug("Replica %d is backup, not sending pre-prepare for request batch %s", instance.id, digest)
	}
	return nil
}

type obcGeneric struct {
	stack consensus.Stack
	pbft  *pbftCore
}

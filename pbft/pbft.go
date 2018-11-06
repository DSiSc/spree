package pbft

import (
	"encoding/base64"
	"fmt"
	logger "github.com/DSiSc/craft/log"
	"github.com/DSiSc/spree/consensus"
	"github.com/DSiSc/spree/pbft/tools"
	"github.com/gogo/protobuf/proto"
	"github.com/spf13/viper"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const configPrefix = "CORE_PBFT"

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
	pset          map[uint64]*ViewChange_PQ
	qset          map[qidx]*ViewChange_PQ

	skipInProgress    bool               // Set when we have detected a fall behind scenario until we pick a new starting point
	stateTransferring bool               // Set when state transfer is executing
	highStateTarget   *stateUpdateTarget // Set to the highest weak checkpoint cert we have observed
	hChkpts           map[uint64]uint64  // highest checkpoint sequence number observed for each replica

	currentExec           *uint64                  // currently executing request
	timerActive           bool                     // is the timer running?
	vcResendTimer         tools.Timer              // timer triggering resend of a view change
	newViewTimer          tools.Timer              // timeout triggering a view change
	requestTimeout        time.Duration            // progress timeout for requests
	vcResendTimeout       time.Duration            // timeout before resending view change
	newViewTimeout        time.Duration            // progress timeout for new views
	newViewTimerReason    string                   // what triggered the timer
	lastNewViewTimeout    time.Duration            // last timeout we used during this view change
	broadcastTimeout      time.Duration            // progress timeout for broadcast
	outstandingReqBatches map[string]*RequestBatch // track whether we are waiting for request batches to execute

	nullRequestTimer   tools.Timer   // timeout triggering a null request
	nullRequestTimeout time.Duration // duration for this timeout
	viewChangePeriod   uint64        // period between automatic view changes
	viewChangeSeqNo    uint64        // next seqNo to perform view change

	missingReqBatches map[string]bool // for all the assigned, non-checkpointed request batches we might be missing during view-change

	// implementation of PBFT `in`
	reqBatchStore   map[string]*RequestBatch // track request batches
	certStore       map[msgID]*msgCert       // track quorum certificates for requests
	checkpointStore map[Checkpoint]bool      // track checkpoints as set
	viewChangeStore map[vcidx]*ViewChange    // track view-change messages
	newViewStore    map[uint64]*NewView      // track last new-view we received or sent
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
	prePrepare  *PrePrepare
	sentPrepare bool
	prepare     []*Prepare
	sentCommit  bool
	commit      []*Commit
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
	execute(seqNo uint64, reqBatch *RequestBatch) // This is invoked on a separate thread
	getState() []byte
	getLastSeqNo() (uint64, error)
	skipTo(seqNo uint64, snapshotID []byte, peers []uint64)

	sign(msg []byte) ([]byte, error)
	verify(senderID uint64, signature []byte, message []byte) error

	invalidateState()
	//validateState()

	consensus.StatePersistor
}

type PeerMap map[uint64]string

var GlobalNodeMap = PeerMap{
	uint64(0): "127.0.0.1:8080",
	uint64(1): "127.0.0.1:8081",
	uint64(2): "127.0.0.1:8082",
	uint64(3): "127.0.0.1:8083",
}

type NodeInfo struct {
	//标示
	Id uint64
	// 监听消息
	Listen *net.TCPListener
	// 发送消息
	Sender *net.TCPAddr
	// 监听端口
	Url string
	// pbft process
	Core *pbftCore
	// peer info
	Peers PeerMap
}

func NewNode(id uint64, url string) *NodeInfo {
	return &NodeInfo{
		Id:    id,
		Url:   url,
		Peers: GlobalNodeMap,
	}
}

func (n *NodeInfo) broadcast(msgPayload []byte) {
	peers := n.Peers
	for id, url := range peers {
		fmt.Printf(">>>>>>>>>>>>>>> sending >>>>>>>>>>>>>>>\n")
		fmt.Printf("Broadcast to node %d with url %s.\n", id, url)
		tcpAddr, err := net.ResolveTCPAddr("tcp4", url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
			os.Exit(1)
		}
		conn, err := net.DialTCP("tcp", nil, tcpAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %s", err.Error())
			os.Exit(1)
		}
		fmt.Printf("connect success and  send to url %s and payload %x.\n", url, msgPayload)
		conn.Write(msgPayload)
		fmt.Printf("send to url %s over.\n", url)
	}
}

func (n *NodeInfo) getLastSeqNo() (uint64, error) {
	return 0, nil
}

func (n *NodeInfo) execute(seqNo uint64, reqBatch *RequestBatch) {
	return
}

func (n *NodeInfo) getState() []byte {
	return nil
}

func (n *NodeInfo) invalidateState() {
	return
}

func (n *NodeInfo) skipTo(seqNo uint64, snapshotID []byte, peers []uint64) {
	return
}

func (n *NodeInfo) unicast(msgPayload []byte, receiverID uint64) (err error) {
	return nil
}

func (n *NodeInfo) verify(senderID uint64, signature []byte, message []byte) error {
	return nil
}

func (n *NodeInfo) sign(msg []byte) ([]byte, error) {
	return nil, nil
}

func (n *NodeInfo) StoreState(key string, value []byte) error {
	return nil
}

func (n *NodeInfo) ReadState(key string) ([]byte, error) {
	return nil, nil
}

func (n *NodeInfo) ReadStateSet(prefix string) (map[string][]byte, error) {
	return nil, nil
}

func (n *NodeInfo) DelState(key string) {
	return
}

// =============================================================================
// constructors
// =============================================================================

func NewPbftCore(id uint64, config *viper.Viper, consumer innerStack, etf tools.TimerFactory) *pbftCore {
	var err error
	instance := &pbftCore{}
	instance.id = id
	instance.consumer = consumer

	instance.newViewTimer = etf.CreateTimer()
	instance.vcResendTimer = etf.CreateTimer()
	instance.nullRequestTimer = etf.CreateTimer()

	instance.N = config.GetInt("general.N")
	instance.f = config.GetInt("general.f")
	if instance.f*3+1 > instance.N {
		panic(fmt.Sprintf("need at least %d enough replicas to tolerate %d byzantine faults, but only %d replicas configured", instance.f*3+1, instance.f, instance.N))
	}

	instance.K = uint64(config.GetInt("general.K"))

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
	instance.broadcastTimeout, err = time.ParseDuration(config.GetString("general.timeout.broadcast"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse new broadcast timeout: %s", err))
	}

	instance.activeView = true
	instance.replicaCount = instance.N

	logger.Info("PBFT type = %T", instance.consumer)
	logger.Info("PBFT Max number of validating peers (N) = %v", instance.N)
	logger.Info("PBFT Max number of failing peers (f) = %v", instance.f)
	logger.Info("PBFT byzantine flag = %v", instance.byzantine)
	logger.Info("PBFT request timeout = %v", instance.requestTimeout)
	logger.Info("PBFT view change timeout = %v", instance.newViewTimeout)
	logger.Info("PBFT Checkpoint period (K) = %v", instance.K)
	logger.Info("PBFT broadcast timeout = %v", instance.broadcastTimeout)
	logger.Info("PBFT Log multiplier = %v", instance.logMultiplier)
	logger.Info("PBFT log size (L) = %v", instance.L)
	if instance.nullRequestTimeout > 0 {
		logger.Info("PBFT null requests timeout = %v", instance.nullRequestTimeout)
	} else {
		logger.Info("PBFT null requests disabled")
	}
	if instance.viewChangePeriod > 0 {
		logger.Info("PBFT view change period = %v", instance.viewChangePeriod)
	} else {
		logger.Info("PBFT automatic view change disabled")
	}

	// init the logs
	instance.certStore = make(map[msgID]*msgCert)
	instance.reqBatchStore = make(map[string]*RequestBatch)
	instance.checkpointStore = make(map[Checkpoint]bool)
	instance.chkpts = make(map[uint64]string)
	instance.viewChangeStore = make(map[vcidx]*ViewChange)
	instance.pset = make(map[uint64]*ViewChange_PQ)
	instance.qset = make(map[qidx]*ViewChange_PQ)
	instance.newViewStore = make(map[uint64]*NewView)

	// initialize state transfer
	instance.hChkpts = make(map[uint64]uint64)

	instance.chkpts[0] = "XXX GENESIS"

	instance.lastNewViewTimeout = instance.newViewTimeout
	instance.outstandingReqBatches = make(map[string]*RequestBatch)
	instance.missingReqBatches = make(map[string]bool)

	instance.restoreState()

	instance.viewChangeSeqNo = ^uint64(0) // infinity
	instance.updateViewChangeSeqNo()

	return instance
}

func (instance *pbftCore) restoreState() {
	updateSeqView := func(set []*ViewChange_PQ) {
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
			reqBatch := &RequestBatch{}
			err = proto.Unmarshal(v, reqBatch)
			if err != nil {
				logger.Warn("Replica %d could not restore request batch %s", instance.id, k)
			} else {
				instance.reqBatchStore[Hash(reqBatch)] = reqBatch
			}
		}
	} else {
		logger.Warn("Replica %d could not restore reqBatchStore: %s", instance.id, err)
	}

	instance.restoreLastSeqNo()

	chkpts, err := instance.consumer.ReadStateSet("chkpt.")
	if err == nil {
		lowWatermark := instance.lastExec // This is safe because we will round down in moveWatermarks
		for key, id := range chkpts {
			var seqNo uint64
			if _, err = fmt.Sscanf(key, "chkpt.%d", &seqNo); err != nil {
				logger.Warn("Replica %d could not restore checkpoint key %s", instance.id, key)
			} else {
				idAsString := base64.StdEncoding.EncodeToString(id)
				logger.Debug("Replica %d found checkpoint %s for seqNo %d", instance.id, idAsString, seqNo)
				instance.chkpts[seqNo] = idAsString
				if seqNo < lowWatermark {
					lowWatermark = seqNo
				}
			}
		}
		instance.moveWatermarks(lowWatermark)
	} else {
		logger.Warn("Replica %d could not restore checkpoints: %s", instance.id, err)
	}

	logger.Info("Replica %d restored state: view: %d, seqNo: %d, pset: %d, qset: %d, reqBatches: %d, chkpts: %d h: %d",
		instance.id, instance.view, instance.seqNo, len(instance.pset), len(instance.qset), len(instance.reqBatchStore), len(instance.chkpts), instance.h)
}

func (instance *pbftCore) restorePQSet(key string) []*ViewChange_PQ {
	raw, err := instance.consumer.ReadState(key)
	if err != nil {
		logger.Debug("Replica %d could not restore state %s: %s", instance.id, key, err)
		return nil
	}
	val := &PQset{}
	err = proto.Unmarshal(raw, val)
	if err != nil {
		logger.Error("Replica %d could not unmarshal %s - local state is damaged: %s", instance.id, key, err)
		return nil
	}
	return val.GetSet()
}

func (instance *pbftCore) restoreLastSeqNo() {
	var err error
	if instance.lastExec, err = instance.consumer.getLastSeqNo(); err != nil {
		logger.Warn("Replica %d could not restore lastExec: %s", instance.id, err)
		instance.lastExec = 0
	}
	logger.Info("Replica %d restored lastExec: %d", instance.id, instance.lastExec)
}

func LoadConfig() (config *viper.Viper) {
	config = viper.New()

	// for environment variables
	config.SetEnvPrefix(configPrefix)
	config.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	config.SetEnvKeyReplacer(replacer)

	config.SetConfigName("config")
	config.AddConfigPath("./")
	config.AddConfigPath("../spree/pbft/")
	// Path to look for the config file in based on GOPATH
	gopath := os.Getenv("GOPATH")
	for _, p := range filepath.SplitList(gopath) {
		pbftpath := filepath.Join(p, "src/github.com/DSiSc/spree/pbft")
		config.AddConfigPath(pbftpath)
	}

	err := config.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Error reading %s plugin config: %s", configPrefix, err))
	}
	return
}

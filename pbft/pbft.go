package pbft

import (
	"github.com/DSiSc/spree/consensus"
	"github.com/DSiSc/spree/pbft/common"
	"github.com/DSiSc/spree/pbft/tools"
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

package pbft

import (
	"fmt"
	"github.com/hyperledger/fabric/consensus"
	"github.com/hyperledger/fabric/consensus/util/events"
	"github.com/spf13/viper"
	"time"
)

type obcBatch struct {
	obcGeneric
	externalEventReceiver
	pbft        *pbftCore
	broadcaster *broadcaster

	batchSize        int
	batchStore       []*Request
	batchTimer       events.Timer
	batchTimerActive bool
	batchTimeout     time.Duration

	manager events.Manager // TODO, remove eventually, the event manager

	incomingChan chan *batchMessage // Queues messages for processing by main thread
	idleChan     chan struct{}      // Idle channel, to be removed

	reqStore *requestStore // Holds the outstanding and pending requests

	deduplicator *deduplicator

	persistForward
}

func newObcBatch(id uint64, config *viper.Viper, stack consensus.Stack) *obcBatch {
	var err error

	op := &obcBatch{
		obcGeneric: obcGeneric{stack: stack},
	}

	op.persistForward.persistor = stack

	logger.Debugf("Replica %d obtaining startup information", id)

	op.manager = events.NewManagerImpl() // TODO, this is hacky, eventually rip it out
	op.manager.SetReceiver(op)
	etf := events.NewTimerFactoryImpl(op.manager)
	op.pbft = newPbftCore(id, config, op, etf)
	op.manager.Start()
	op.externalEventReceiver.manager = op.manager
	op.broadcaster = newBroadcaster(id, op.pbft.N, op.pbft.f, stack)

	op.batchSize = config.GetInt("general.batchsize")
	op.batchStore = nil
	op.batchTimeout, err = time.ParseDuration(config.GetString("general.timeout.batch"))
	if err != nil {
		panic(fmt.Errorf("Cannot parse batch timeout: %s", err))
	}
	logger.Infof("PBFT Batch size = %d", op.batchSize)
	logger.Infof("PBFT Batch timeout = %v", op.batchTimeout)

	if op.batchTimeout >= op.pbft.requestTimeout {
		op.pbft.requestTimeout = 3 * op.batchTimeout / 2
		logger.Warningf("Configured request timeout must be greater than batch timeout, setting to %v", op.pbft.requestTimeout)
	}

	if op.pbft.requestTimeout >= op.pbft.nullRequestTimeout && op.pbft.nullRequestTimeout != 0 {
		op.pbft.nullRequestTimeout = 3 * op.pbft.requestTimeout / 2
		logger.Warningf("Configured null request timeout must be greater than request timeout, setting to %v", op.pbft.nullRequestTimeout)
	}

	op.incomingChan = make(chan *batchMessage)

	op.batchTimer = etf.CreateTimer()

	op.reqStore = newRequestStore()

	op.deduplicator = newDeduplicator()

	op.idleChan = make(chan struct{})
	close(op.idleChan) // TODO remove eventually

	return op
}

package consensus

// StatePersistor is used to store consensus state which should survive a process crash
type StatePersistor interface {
	StoreState(key string, value []byte) error
	ReadState(key string) ([]byte, error)
	ReadStateSet(prefix string) (map[string][]byte, error)
	DelState(key string)
}

// Consenter is used to receive messages from the network
// Every consensus plugin needs to implement this interface
type Consenter interface {
	RecvMsg(msg *pb.Message, senderHandle *pb.PeerID) error // Called serially with incoming messages from gRPC
	ExecutionConsumer
}

// Inquirer is used to retrieve info about the validating network
type Inquirer interface {
	GetNetworkInfo() (self *pb.PeerEndpoint, network []*pb.PeerEndpoint, err error)
	GetNetworkHandles() (self *pb.PeerID, network []*pb.PeerID, err error)
}

// Communicator is used to send messages to other validators
type Communicator interface {
	Broadcast(msg *pb.Message, peerType pb.PeerEndpoint_Type) error
	Unicast(msg *pb.Message, receiverHandle *pb.PeerID) error
}

// NetworkStack is used to retrieve network info and send messages
type NetworkStack interface {
	Communicator
	Inquirer
}

// Stack is the set of stack-facing methods available to the consensus plugin
type Stack interface {
	NetworkStack
	SecurityUtils
	Executor
	LegacyExecutor
	LedgerManager
	ReadOnlyLedger
	StatePersistor
}

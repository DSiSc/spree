package main

import (
	"flag"
	"fmt"
	"github.com/DSiSc/spree/pbft"
	"github.com/DSiSc/spree/pbft/tools"
	"time"
)

func main() {
	// NewNode
	nodeId := flag.Uint64("node", 0, "node id")
	flag.Parse()
	fmt.Printf("I am %d.\n", *nodeId)
	nodeInfo := pbft.NewNode(*nodeId, pbft.GlobalNodeMap[*nodeId])
	nodeInfo.Core = pbft.NewPbftCore(nodeInfo.Id, pbft.LoadConfig(), nodeInfo, &pbft.InertTimerFactory{})
	go pbft.Server(nodeInfo.Core)
	primary := int64(0)
	index := uint64(0)
	for {
		if index == 10 {
			break
		}
		if uint64(primary) == *nodeId {
			reqBatch := pbft.CreatePbftReqBatch(int64(index), 0)
			req := reqBatch.Batch[0]
			fmt.Printf("I will send:\nReplicaId: %v.\nPayload: %v.\nSignature: %v.\nTimestamp: %v.\n", req.ReplicaId, req.Payload, req.Signature, req.Timestamp)
			tools.SendEvent(nodeInfo.Core, reqBatch)
			fmt.Printf("+++++++++++++++++++++++++++\n")
		}
		time.Sleep(20 * time.Second)
		index += 1
		fmt.Printf("+++++++++++++%d+++++++++++++\n", index)
	}

	time.Sleep(1000000 * time.Second)
}

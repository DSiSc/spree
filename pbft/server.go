package pbft

import (
	"fmt"
	"github.com/DSiSc/spree/pbft/tools"
	"github.com/gogo/protobuf/proto"
	"net"
)

func Server(bft *pbftCore) {
	url := GlobalNodeMap[bft.id]
	localAddress, _ := net.ResolveTCPAddr("tcp4", url)
	var tcpListener, err = net.ListenTCP("tcp", localAddress)
	if err != nil {
		fmt.Println("listen error：", err)
		return
	}
	defer func() {
		tcpListener.Close()
	}()
	fmt.Println("waitting to be connected ...")
	handleConnection(tcpListener, bft)
}

func handleConnection(tcpListener *net.TCPListener, bft *pbftCore) {
	buffer := make([]byte, 2048)
	for {
		var conn, _ = tcpListener.AcceptTCP()
		n, err := conn.Read(buffer)
		// var buffer, err = ioutil.ReadAll(conn)
		if err != nil {
			fmt.Printf("ERROR：%x.\n", err)
			return
		}
		fmt.Printf("<<<<<<<<<<<<<< receive request message <<<<<<<<<<<<<<<\n")
		var msg Message
		err = proto.Unmarshal(buffer[:n], &msg)
		payload := msg.GetPayload()
		switch payload.(type) {
		case *Message_RequestBatch:
			request := payload.(*Message_RequestBatch).RequestBatch
			tools.SendEvent(bft, request)
		case *Message_PrePrepare:
			fmt.Printf("receive preprepare message: ")
			prePrepare := payload.(*Message_PrePrepare).PrePrepare
			fmt.Printf("receive preprepare message: \n view: %v.\n digest: %v.\n id: %v.\n request: %v.\n seqNo: %v.\n",
				prePrepare.View, prePrepare.BatchDigest, prePrepare.ReplicaId, prePrepare.RequestBatch, prePrepare.SequenceNumber)
			fmt.Printf("receive preprepare message: len is %d. \n", len(prePrepare.RequestBatch.Batch))
			req := prePrepare.RequestBatch.Batch[0]
			fmt.Printf("receive preprepare message: ReplicaId: %v.\n Payload: %v.\n Signature: %v.\n Timestamp: %v.\n", req.ReplicaId, req.Payload, req.Signature, req.Timestamp)
			fmt.Printf("receive preprepare message: Begin trying to send prepare message to event process.\n")
			tools.SendEvent(bft, prePrepare)
		case *Message_Prepare:
			prepare := payload.(*Message_Prepare).Prepare
			fmt.Printf("receive prepare message: \n view: %v.\n digest: %v.\n id: %v.\n seqNo: %v.\n",
				prepare.View, prepare.BatchDigest, prepare.ReplicaId, prepare.SequenceNumber)
			fmt.Printf("receive prepare message: Begin trying to send commit message to event process.\n")
			tools.SendEvent(bft, prepare)
		case *Message_Commit:
			commit := payload.(*Message_Commit).Commit
			fmt.Printf("receive commit message: \n view: %v.\n digest: %v.\n id: %v.\n seqNo: %v.\n",
				commit.View, commit.BatchDigest, commit.ReplicaId, commit.SequenceNumber)
			fmt.Printf("receive commit message: Begin trying to confirm the commit message to event process.\n")
			tools.SendEvent(bft, commit)
		default:
			fmt.Print("Unsupport message type.\n")
			break
		}

	}
}

package common

import (
	"github.com/golang/protobuf/proto"
	google_protobuf "github.com/golang/protobuf/ptypes/timestamp"
)

type Message struct {
	// Types that are valid to be assigned to Payload:
	//	*Message_RequestBatch
	//	*Message_PrePrepare
	//	*Message_Prepare
	//	*Message_Commit
	//	*Message_Checkpoint
	//	*Message_ViewChange
	//	*Message_NewView
	//	*Message_FetchRequestBatch
	//	*Message_ReturnRequestBatch
	Payload isMessage_Payload `protobuf_oneof:"payload"`
}

func (m *Message) Reset()         { *m = Message{} }
func (m *Message) String() string { return proto.CompactTextString(m) }
func (*Message) ProtoMessage()    {}

type isMessage_Payload interface {
	isMessage_Payload()
}

type Message_RequestBatch struct {
	RequestBatch *RequestBatch `protobuf:"bytes,1,opt,name=request_batch,json=requestBatch,oneof"`
}

type Message_PrePrepare struct {
	PrePrepare *PrePrepare `protobuf:"bytes,2,opt,name=pre_prepare,json=prePrepare,oneof"`
}

func (m *PrePrepare) GetRequestBatch() *RequestBatch {
	if m != nil {
		return m.RequestBatch
	}
	return nil
}

type Message_Prepare struct {
	Prepare *Prepare `protobuf:"bytes,3,opt,name=prepare,oneof"`
}

type Message_Commit struct {
	Commit *Commit `protobuf:"bytes,4,opt,name=commit,oneof"`
}

type Message_ViewChange struct {
	ViewChange *ViewChange `protobuf:"bytes,6,opt,name=view_change,json=viewChange,oneof"`
}

type Message_Checkpoint struct {
	Checkpoint *Checkpoint `protobuf:"bytes,5,opt,name=checkpoint,oneof"`
}

type Message_FetchRequestBatch struct {
	FetchRequestBatch *FetchRequestBatch `protobuf:"bytes,8,opt,name=fetch_request_batch,json=fetchRequestBatch,oneof"`
}

func (*Message_RequestBatch) isMessage_Payload()      {}
func (*Message_PrePrepare) isMessage_Payload()        {}
func (*Message_Prepare) isMessage_Payload()           {}
func (*Message_Commit) isMessage_Payload()            {}
func (*Message_ViewChange) isMessage_Payload()        {}
func (*Message_Checkpoint) isMessage_Payload()        {}
func (*Message_FetchRequestBatch) isMessage_Payload() {}

type ViewChange_PQ struct {
	SequenceNumber uint64 `protobuf:"varint,1,opt,name=sequence_number" json:"sequence_number,omitempty"`
	BatchDigest    string `protobuf:"bytes,2,opt,name=batch_digest" json:"batch_digest,omitempty"`
	View           uint64 `protobuf:"varint,3,opt,name=view" json:"view,omitempty"`
}

type Request struct {
	Timestamp *google_protobuf.Timestamp `protobuf:"bytes,1,opt,name=timestamp" json:"timestamp,omitempty"`
	Payload   []byte                     `protobuf:"bytes,2,opt,name=payload,proto3" json:"payload,omitempty"`
	ReplicaId uint64                     `protobuf:"varint,3,opt,name=replica_id" json:"replica_id,omitempty"`
	Signature []byte                     `protobuf:"bytes,4,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (m *Request) Reset()         { *m = Request{} }
func (m *Request) String() string { return proto.CompactTextString(m) }
func (*Request) ProtoMessage()    {}

func (m *Request) GetTimestamp() *google_protobuf.Timestamp {
	if m != nil {
		return m.Timestamp
	}
	return nil
}

type RequestBatch struct {
	Batch []*Request `protobuf:"bytes,1,rep,name=batch" json:"batch,omitempty"`
}

func (m *RequestBatch) Reset()         { *m = RequestBatch{} }
func (m *RequestBatch) String() string { return proto.CompactTextString(m) }
func (*RequestBatch) ProtoMessage()    {}

func (m *RequestBatch) GetBatch() []*Request {
	if m != nil {
		return m.Batch
	}
	return nil
}

type Prepare struct {
	View           uint64 `protobuf:"varint,1,opt,name=view" json:"view,omitempty"`
	SequenceNumber uint64 `protobuf:"varint,2,opt,name=sequence_number" json:"sequence_number,omitempty"`
	BatchDigest    string `protobuf:"bytes,3,opt,name=batch_digest" json:"batch_digest,omitempty"`
	ReplicaId      uint64 `protobuf:"varint,4,opt,name=replica_id" json:"replica_id,omitempty"`
}

type PrePrepare struct {
	View           uint64        `protobuf:"varint,1,opt,name=view" json:"view,omitempty"`
	SequenceNumber uint64        `protobuf:"varint,2,opt,name=sequence_number" json:"sequence_number,omitempty"`
	BatchDigest    string        `protobuf:"bytes,3,opt,name=batch_digest" json:"batch_digest,omitempty"`
	RequestBatch   *RequestBatch `protobuf:"bytes,4,opt,name=request_batch" json:"request_batch,omitempty"`
	ReplicaId      uint64        `protobuf:"varint,5,opt,name=replica_id" json:"replica_id,omitempty"`
}

type Commit struct {
	View           uint64 `protobuf:"varint,1,opt,name=view" json:"view,omitempty"`
	SequenceNumber uint64 `protobuf:"varint,2,opt,name=sequence_number" json:"sequence_number,omitempty"`
	BatchDigest    string `protobuf:"bytes,3,opt,name=batch_digest" json:"batch_digest,omitempty"`
	ReplicaId      uint64 `protobuf:"varint,4,opt,name=replica_id" json:"replica_id,omitempty"`
}

// This message should go away and become a checkpoint once replica_id is removed
type ViewChange_C struct {
	SequenceNumber uint64 `protobuf:"varint,1,opt,name=sequence_number" json:"sequence_number,omitempty"`
	Id             string `protobuf:"bytes,3,opt,name=id" json:"id,omitempty"`
}

type ViewChange struct {
	View      uint64           `protobuf:"varint,1,opt,name=view" json:"view,omitempty"`
	H         uint64           `protobuf:"varint,2,opt,name=h" json:"h,omitempty"`
	Cset      []*ViewChange_C  `protobuf:"bytes,3,rep,name=cset" json:"cset,omitempty"`
	Pset      []*ViewChange_PQ `protobuf:"bytes,4,rep,name=pset" json:"pset,omitempty"`
	Qset      []*ViewChange_PQ `protobuf:"bytes,5,rep,name=qset" json:"qset,omitempty"`
	ReplicaId uint64           `protobuf:"varint,6,opt,name=replica_id" json:"replica_id,omitempty"`
	Signature []byte           `protobuf:"bytes,7,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (vc *ViewChange) Reset()         { *vc = ViewChange{} }
func (vc *ViewChange) String() string { return proto.CompactTextString(vc) }
func (*ViewChange) ProtoMessage()     {}

func (vc *ViewChange) getSignature() []byte {
	return vc.Signature
}

func (vc *ViewChange) setSignature(sig []byte) {
	vc.Signature = sig
}

func (vc *ViewChange) setID(id uint64) {
	vc.ReplicaId = id
}

func (vc *ViewChange) getID() uint64 {
	return vc.ReplicaId
}

func (vc *ViewChange) serialize() ([]byte, error) {
	return proto.Marshal(vc)
}

type NewView struct {
	View      uint64            `protobuf:"varint,1,opt,name=view" json:"view,omitempty"`
	Vset      []*ViewChange     `protobuf:"bytes,2,rep,name=vset" json:"vset,omitempty"`
	Xset      map[uint64]string `protobuf:"bytes,3,rep,name=xset" json:"xset,omitempty" protobuf_key:"varint,1,opt,name=key" protobuf_val:"bytes,2,opt,name=value"`
	ReplicaId uint64            `protobuf:"varint,4,opt,name=replica_id" json:"replica_id,omitempty"`
}

type Checkpoint struct {
	SequenceNumber uint64 `protobuf:"varint,1,opt,name=sequence_number" json:"sequence_number,omitempty"`
	ReplicaId      uint64 `protobuf:"varint,2,opt,name=replica_id" json:"replica_id,omitempty"`
	Id             string `protobuf:"bytes,3,opt,name=id" json:"id,omitempty"`
}

type PQset struct {
	Set []*ViewChange_PQ `protobuf:"bytes,1,rep,name=set" json:"set,omitempty"`
}

func (m *PQset) Reset()         { *m = PQset{} }
func (m *PQset) String() string { return proto.CompactTextString(m) }
func (*PQset) ProtoMessage()    {}

type FetchRequestBatch struct {
	BatchDigest string `protobuf:"bytes,1,opt,name=batch_digest,json=batchDigest" json:"batch_digest,omitempty"`
	ReplicaId   uint64 `protobuf:"varint,2,opt,name=replica_id,json=replicaId" json:"replica_id,omitempty"`
}

func (m *FetchRequestBatch) Reset()         { *m = FetchRequestBatch{} }
func (m *FetchRequestBatch) String() string { return proto.CompactTextString(m) }
func (*FetchRequestBatch) ProtoMessage()    {}

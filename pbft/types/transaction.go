package types

import (
	google_protobuf "github.com/golang/protobuf/proto"
	gtime "github.com/golang/protobuf/ptypes/timestamp"
)

type Transaction_Type int32

// Confidentiality Levels
type ConfidentialityLevel int32

type Transaction struct {
	Type Transaction_Type `protobuf:"varint,1,opt,name=type,enum=protos.Transaction_Type" json:"type,omitempty"`
	// store ChaincodeID as bytes so its encrypted value can be stored
	ChaincodeID                    []byte               `protobuf:"bytes,2,opt,name=chaincodeID,proto3" json:"chaincodeID,omitempty"`
	Payload                        []byte               `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
	Metadata                       []byte               `protobuf:"bytes,4,opt,name=metadata,proto3" json:"metadata,omitempty"`
	Txid                           string               `protobuf:"bytes,5,opt,name=txid" json:"txid,omitempty"`
	Timestamp                      *gtime.Timestamp     `protobuf:"bytes,6,opt,name=timestamp" json:"timestamp,omitempty"`
	ConfidentialityLevel           ConfidentialityLevel `protobuf:"varint,7,opt,name=confidentialityLevel,enum=protos.ConfidentialityLevel" json:"confidentialityLevel,omitempty"`
	ConfidentialityProtocolVersion string               `protobuf:"bytes,8,opt,name=confidentialityProtocolVersion" json:"confidentialityProtocolVersion,omitempty"`
	Nonce                          []byte               `protobuf:"bytes,9,opt,name=nonce,proto3" json:"nonce,omitempty"`
	ToValidators                   []byte               `protobuf:"bytes,10,opt,name=toValidators,proto3" json:"toValidators,omitempty"`
	Cert                           []byte               `protobuf:"bytes,11,opt,name=cert,proto3" json:"cert,omitempty"`
	Signature                      []byte               `protobuf:"bytes,12,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (m *Transaction) Reset()         { *m = Transaction{} }
func (m *Transaction) String() string { return google_protobuf.CompactTextString(m) }
func (*Transaction) ProtoMessage()    {}

func (m *Transaction) GetTimestamp() *gtime.Timestamp {
	if m != nil {
		return m.Timestamp
	}
	return nil
}

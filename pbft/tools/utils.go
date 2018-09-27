package tools

import (
	"encoding/base64"
	"github.com/DSiSc/craft/log"
	"github.com/DSiSc/crypto-suite/crypto/sha3"
	"github.com/DSiSc/spree/pbft/common"
	"github.com/golang/protobuf/proto"
)

func Hash(msg interface{}) string {
	var raw []byte
	switch converted := msg.(type) {
	case *common.Request:
		raw, _ = proto.Marshal(converted)
	case *common.RequestBatch:
		raw, _ = proto.Marshal(converted)
	default:
		log.Error("Asked to hash non-supported message type, ignoring")
		return ""
	}
	return base64.StdEncoding.EncodeToString(ComputeCryptoHash(raw))

}

// ComputeCryptoHash should be used in openchain code so that we can change the actual algo used for crypto-hash at one place
func ComputeCryptoHash(data []byte) (hash []byte) {
	hash = make([]byte, 64)
	sha3.ShakeSum256(hash, data)
	return
}

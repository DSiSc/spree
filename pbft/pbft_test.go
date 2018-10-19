package pbft

/*
func Test_loadConfig(t *testing.T) {
	assert := assert.New(t)
	err := loadConfig()
	assert.NotNil(err)
}

func Test_newPbftCore(t *testing.T) {
	primary := int64(0)
	validator := uint64(1)
	validatorCount := 4
	sender := uint64(generateBroadcaster(validatorCount))
	// reqBatch
	reqBatch := CreatePbftReqBatch(primary, sender)
	//instance := NewPbftCore(1, loadConfig(), &omniProto{}, &inertTimerFactory{})
	// tools.SendEvent(instance, reqBatch)
	/*
		// preprepare
		preprepare := &PrePrepare{
			View:           0,
			SequenceNumber: 1,
			BatchDigest:    Hash(reqBatch),
			RequestBatch:   reqBatch,
			ReplicaId:      uint64(0),
		}
	    tools.SendEvent(instance, preprepare)

	// prepare
	prepare := &Prepare{
		View:           0,
		SequenceNumber: 1,
		BatchDigest:    Hash(reqBatch),
		ReplicaId:      validator, //  primary will not send prepare message
	}
	tools.SendEvent(instance, prepare)
	// commit
	commit := &Commit{
		View:           0,
		SequenceNumber: 1,
		BatchDigest:    Hash(reqBatch),
		ReplicaId:      uint64(primary),
	}
	tools.SendEvent(instance, commit)
}
*/

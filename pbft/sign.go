package pbft

type signable interface {
	getSignature() []byte
	setSignature(s []byte)
	getID() uint64
	setID(id uint64)
	serialize() ([]byte, error)
}

func (instance *pbftCore) verify(s signable) error {
	origSig := s.getSignature()
	s.setSignature(nil)
	raw, err := s.serialize()
	s.setSignature(origSig)
	if err != nil {
		return err
	}
	return instance.consumer.verify(s.getID(), origSig, raw)
}

func (instance *pbftCore) sign(s signable) error {
	s.setSignature(nil)
	raw, err := s.serialize()
	if err != nil {
		return err
	}
	signedRaw, err := instance.consumer.sign(raw)
	if err != nil {
		return err
	}
	s.setSignature(signedRaw)

	return nil
}

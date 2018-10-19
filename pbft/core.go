package pbft

import (
	"encoding/base64"
	"fmt"
	logger "github.com/DSiSc/craft/log"
	"github.com/DSiSc/spree/pbft/tools"
	"github.com/golang/protobuf/proto"
	"math/rand"
	"reflect"
	"sort"
	"time"
)

// viewChangeTimerEvent is sent when the view change timer expires
type viewChangeTimerEvent struct{}

// viewChangeResendTimerEvent is sent when the view change resend timer expires
type viewChangeResendTimerEvent struct{}

// viewChangeQuorumEvent is returned to the event loop when a new ViewChange message is received which is part of a quorum cert
type viewChangeQuorumEvent struct{}

// nullRequestEvent provides "keep-alive" null requests
type nullRequestEvent struct{}

// viewChangedEvent is sent when the view change timer expires
type viewChangedEvent struct{}

type sortableUint64Slice []uint64

func (a sortableUint64Slice) Len() int {
	return len(a)
}
func (a sortableUint64Slice) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
func (a sortableUint64Slice) Less(i, j int) bool {
	return a[i] < a[j]
}

// allow the view-change protocol to kick-off when the timer expires
func (instance *pbftCore) ProcessEvent(e tools.Event) tools.Event {
	var err error
	logger.Debug("Replica %d processing event", instance.id)
	switch et := e.(type) {
	case *RequestBatch:
		fmt.Println("******* 1 ********")
		err = instance.recvRequestBatch(et)
	case *PrePrepare:
		fmt.Println("******* 2 ********")
		err = instance.recvPrePrepare(et)
	case *Prepare:
		fmt.Println("******* 3 ********")
		err = instance.recvPrepare(et)
	case *Commit:
		fmt.Println("******* 4 ********")
		err = instance.recvCommit(et)
	case *Checkpoint:
		return instance.recvCheckpoint(et)
	case *ViewChange:
		return instance.recvViewChange(et)
	case *NewView:
		return instance.recvNewView(et)
	default:
		logger.Warn("Replica %d received an unknown message type %T", instance.id, et)
	}

	if err != nil {
		logger.Warn(err.Error())
	}

	return nil
}

// =============================================================================
// receive methods
// =============================================================================

// Process received batch request
func (instance *pbftCore) recvRequestBatch(reqBatch *RequestBatch) error {
	digest := Hash(reqBatch)
	logger.Debug("Replica %d received request batch %s", instance.id, digest)

	instance.reqBatchStore[digest] = reqBatch
	instance.outstandingReqBatches[digest] = reqBatch
	instance.persistRequestBatch(digest)
	if instance.activeView {
		instance.softStartTimer(instance.requestTimeout, fmt.Sprintf("new request batch %s", digest))
	}
	if instance.primary(instance.view) == instance.id && instance.activeView {
		instance.nullRequestTimer.Stop()
		instance.sendPrePrepare(reqBatch, digest)
	} else {
		logger.Debug("Replica %d is backup, not sending pre-prepare for request batch %s", instance.id, digest)
	}
	return nil
}

// Persist batch request message
func (instance *pbftCore) persistRequestBatch(digest string) {
	reqBatch := instance.reqBatchStore[digest]
	reqBatchPacked, err := proto.Marshal(reqBatch)
	if err != nil {
		logger.Warn("Replica %d could not persist request batch %s: %s", instance.id, digest, err)
		return
	}
	err = instance.consumer.StoreState("reqBatch."+digest, reqBatchPacked)
	if err != nil {
		logger.Warn("Replica %d could not persist request batch %s: %s", instance.id, digest, err)
	}
}

// Receive pre-prepare message
func (instance *pbftCore) recvPrePrepare(preprep *PrePrepare) error {
	logger.Debug("Replica %d received pre-prepare from replica %d for view=%d/seqNo=%d",
		instance.id, preprep.ReplicaId, preprep.View, preprep.SequenceNumber)

	if !instance.activeView {
		logger.Debug("Replica %d ignoring pre-prepare as we are in a view change", instance.id)
		return nil
	}

	if instance.primary(instance.view) != preprep.ReplicaId {
		logger.Warn("Pre-prepare from other than primary: got %d, should be %d", preprep.ReplicaId, instance.primary(instance.view))
		return nil
	}

	if !instance.inWV(preprep.View, preprep.SequenceNumber) {
		if preprep.SequenceNumber != instance.h && !instance.skipInProgress {
			logger.Warn("Replica %d pre-prepare view different, or sequence number outside watermarks: preprep.View %d, expected.View %d, seqNo %d, low-mark %d", instance.id, preprep.View, instance.primary(instance.view), preprep.SequenceNumber, instance.h)
		} else {
			// This is perfectly normal
			logger.Debug("Replica %d pre-prepare view different, or sequence number outside watermarks: preprep.View %d, expected.View %d, seqNo %d, low-mark %d", instance.id, preprep.View, instance.primary(instance.view), preprep.SequenceNumber, instance.h)
		}

		return nil
	}

	if preprep.SequenceNumber > instance.viewChangeSeqNo {
		logger.Info("Replica %d received pre-prepare for %d, which should be from the next primary", instance.id, preprep.SequenceNumber)
		instance.sendViewChange()
		return nil
	}

	cert := instance.getCert(preprep.View, preprep.SequenceNumber)
	if cert.digest != "" && cert.digest != preprep.BatchDigest {
		logger.Warn("Pre-prepare found for same view/seqNo but different digest: received %s, stored %s", preprep.BatchDigest, cert.digest)
		instance.sendViewChange()
		return nil
	}

	cert.prePrepare = preprep
	cert.digest = preprep.BatchDigest

	// Store the request batch if, for whatever reason, we haven't received it from an earlier broadcast
	if _, ok := instance.reqBatchStore[preprep.BatchDigest]; !ok && preprep.BatchDigest != "" {
		digest := Hash(preprep.GetRequestBatch())
		if digest != preprep.BatchDigest {
			logger.Warn("Pre-prepare and request digest do not match: request %s, digest %s", digest, preprep.BatchDigest)
			return nil
		}
		instance.reqBatchStore[digest] = preprep.GetRequestBatch()
		logger.Debug("Replica %d storing request batch %s in outstanding request batch store", instance.id, digest)
		instance.outstandingReqBatches[digest] = preprep.GetRequestBatch()
		instance.persistRequestBatch(digest)
	}

	instance.softStartTimer(instance.requestTimeout, fmt.Sprintf("new pre-prepare for request batch %s", preprep.BatchDigest))
	instance.nullRequestTimer.Stop()

	if instance.primary(instance.view) != instance.id && instance.prePrepared(preprep.BatchDigest, preprep.View, preprep.SequenceNumber) && !cert.sentPrepare {
		logger.Debug("Backup %d broadcasting prepare for view=%d/seqNo=%d", instance.id, preprep.View, preprep.SequenceNumber)
		prep := &Prepare{
			View:           preprep.View,
			SequenceNumber: preprep.SequenceNumber,
			BatchDigest:    preprep.BatchDigest,
			ReplicaId:      instance.id,
		}
		cert.sentPrepare = true
		instance.persistQSet()
		instance.recvPrepare(prep)
		return instance.innerBroadcast(&Message{Payload: &Message_Prepare{Prepare: prep}})
	}

	return nil
}

// Receive prepare message
func (instance *pbftCore) recvPrepare(prep *Prepare) error {
	logger.Debug("Replica %d received prepare from replica %d for view=%d/seqNo=%d",
		instance.id, prep.ReplicaId, prep.View, prep.SequenceNumber)

	if instance.primary(prep.View) == prep.ReplicaId {
		logger.Warn("Replica %d received prepare from primary, ignoring", instance.id)
		return nil
	}

	if !instance.inWV(prep.View, prep.SequenceNumber) {
		if prep.SequenceNumber != instance.h && !instance.skipInProgress {
			logger.Warn("Replica %d ignoring prepare for view=%d/seqNo=%d: not in-wv, in view %d, low water mark %d", instance.id, prep.View, prep.SequenceNumber, instance.view, instance.h)
		} else {
			// This is perfectly normal
			logger.Debug("Replica %d ignoring prepare for view=%d/seqNo=%d: not in-wv, in view %d, low water mark %d", instance.id, prep.View, prep.SequenceNumber, instance.view, instance.h)
		}
		return nil
	}

	cert := instance.getCert(prep.View, prep.SequenceNumber)

	for _, prevPrep := range cert.prepare {
		if prevPrep.ReplicaId == prep.ReplicaId {
			logger.Warn("Ignoring duplicate prepare from %d", prep.ReplicaId)
			return nil
		}
	}
	cert.prepare = append(cert.prepare, prep)
	instance.persistPSet()

	return instance.maybeSendCommit(prep.BatchDigest, prep.View, prep.SequenceNumber)
}

// Receive commit message
func (instance *pbftCore) recvCommit(commit *Commit) error {
	logger.Debug("Replica %d received commit from replica %d for view=%d/seqNo=%d",
		instance.id, commit.ReplicaId, commit.View, commit.SequenceNumber)

	if !instance.inWV(commit.View, commit.SequenceNumber) {
		if commit.SequenceNumber != instance.h && !instance.skipInProgress {
			logger.Warn("Replica %d ignoring commit for view=%d/seqNo=%d: not in-wv, in view %d, low water mark %d",
				instance.id, commit.View, commit.SequenceNumber, instance.view, instance.h)
		} else {
			// This is perfectly normal
			logger.Debug("Replica %d ignoring commit for view=%d/seqNo=%d: not in-wv, in view %d, low water mark %d",
				instance.id, commit.View, commit.SequenceNumber, instance.view, instance.h)
		}
		return nil
	}

	cert := instance.getCert(commit.View, commit.SequenceNumber)
	for _, prevCommit := range cert.commit {
		if prevCommit.ReplicaId == commit.ReplicaId {
			logger.Warn("Ignoring duplicate commit from %d", commit.ReplicaId)
			return nil
		}
	}
	cert.commit = append(cert.commit, commit)

	if instance.committed(commit.BatchDigest, commit.View, commit.SequenceNumber) {
		instance.stopTimer()
		instance.lastNewViewTimeout = instance.newViewTimeout
		delete(instance.outstandingReqBatches, commit.BatchDigest)

		instance.executeOutstanding()

		if commit.SequenceNumber == instance.viewChangeSeqNo {
			logger.Info("Replica %d cycling view for seqNo=%d", instance.id, commit.SequenceNumber)
			instance.sendViewChange()
		}
	}

	return nil
}

// Receive ViewChange message
func (instance *pbftCore) recvViewChange(vc *ViewChange) tools.Event {
	logger.Info("Replica %d received view-change from replica %d, v:%d, h:%d, |C|:%d, |P|:%d, |Q|:%d",
		instance.id, vc.ReplicaId, vc.View, vc.H, len(vc.Cset), len(vc.Pset), len(vc.Qset))

	if err := instance.verify(vc); err != nil {
		logger.Warn("Replica %d found incorrect signature in view-change message: %s", instance.id, err)
		return nil
	}

	if vc.View < instance.view {
		logger.Warn("Replica %d found view-change message for old view", instance.id)
		return nil
	}

	if !instance.correctViewChange(vc) {
		logger.Warn("Replica %d found view-change message incorrect", instance.id)
		return nil
	}

	if _, ok := instance.viewChangeStore[vcidx{vc.View, vc.ReplicaId}]; ok {
		logger.Warn("Replica %d already has a view change message for view %d from replica %d", instance.id, vc.View, vc.ReplicaId)
		return nil
	}

	instance.viewChangeStore[vcidx{vc.View, vc.ReplicaId}] = vc

	// PBFT TOCS 4.5.1 Liveness: "if a replica receives a set of
	// f+1 valid VIEW-CHANGE messages from other replicas for
	// views greater than its current view, it sends a VIEW-CHANGE
	// message for the smallest view in the set, even if its timer
	// has not expired"
	replicas := make(map[uint64]bool)
	minView := uint64(0)
	for idx := range instance.viewChangeStore {
		if idx.v <= instance.view {
			continue
		}

		replicas[idx.id] = true
		if minView == 0 || idx.v < minView {
			minView = idx.v
		}
	}

	// We only enter this if there are enough view change messages _greater_ than our current view
	if len(replicas) >= instance.f+1 {
		logger.Info("Replica %d received f+1 view-change messages, triggering view-change to view %d",
			instance.id, minView)
		// subtract one, because sendViewChange() increments
		instance.view = minView - 1
		return instance.sendViewChange()
	}

	quorum := 0
	for idx := range instance.viewChangeStore {
		if idx.v == instance.view {
			quorum++
		}
	}
	logger.Debug("Replica %d now has %d view change requests for view %d", instance.id, quorum, instance.view)

	if !instance.activeView && vc.View == instance.view && quorum >= instance.allCorrectReplicasQuorum() {
		instance.vcResendTimer.Stop()
		instance.startTimer(instance.lastNewViewTimeout, "new view change")
		instance.lastNewViewTimeout = 2 * instance.lastNewViewTimeout
		return viewChangeQuorumEvent{}
	}

	return nil
}

// Receive NewView message
func (instance *pbftCore) recvNewView(nv *NewView) tools.Event {
	logger.Info("Replica %d received new-view %d",
		instance.id, nv.View)

	if !(nv.View > 0 && nv.View >= instance.view && instance.primary(nv.View) == nv.ReplicaId && instance.newViewStore[nv.View] == nil) {
		logger.Info("Replica %d rejecting invalid new-view from %d, v:%d",
			instance.id, nv.ReplicaId, nv.View)
		return nil
	}

	for _, vc := range nv.Vset {
		if err := instance.verify(vc); err != nil {
			logger.Warn("Replica %d found incorrect view-change signature in new-view message: %s", instance.id, err)
			return nil
		}
	}

	instance.newViewStore[nv.View] = nv
	return instance.processNewView()
}

// Execute batch requests
func (instance *pbftCore) executeOutstanding() {
	if instance.currentExec != nil {
		logger.Debug("Replica %d not attempting to executeOutstanding because it is currently executing %d", instance.id, *instance.currentExec)
		return
	}
	logger.Debug("Replica %d attempting to executeOutstanding", instance.id)

	for idx := range instance.certStore {
		if instance.executeOne(idx) {
			break
		}
	}

	logger.Debug("Replica %d certstore %+v", instance.id, instance.certStore)

	instance.startTimerIfOutstandingRequests()
}

// Execute one request
func (instance *pbftCore) executeOne(idx msgID) bool {
	cert := instance.certStore[idx]

	if idx.n != instance.lastExec+1 || cert == nil || cert.prePrepare == nil {
		return false
	}

	if instance.skipInProgress {
		logger.Debug("Replica %d currently picking a starting point to resume, will not execute", instance.id)
		return false
	}

	// we now have the right sequence number that doesn't create holes

	digest := cert.digest
	reqBatch := instance.reqBatchStore[digest]

	if !instance.committed(digest, idx.v, idx.n) {
		return false
	}

	// we have a commit certificate for this request batch
	currentExec := idx.n
	instance.currentExec = &currentExec

	// null request
	if digest == "" {
		logger.Info("Replica %d executing/committing null request for view=%d/seqNo=%d",
			instance.id, idx.v, idx.n)
		instance.execDoneSync()
	} else {
		logger.Info("Replica %d executing/committing request batch for view=%d/seqNo=%d and digest %s",
			instance.id, idx.v, idx.n, digest)
		// synchronously execute, it is the other side's responsibility to execute in the background if needed
		instance.consumer.execute(idx.n, reqBatch)
	}
	return true
}

func (instance *pbftCore) execDoneSync() {
	if instance.currentExec != nil {
		logger.Info("Replica %d finished execution %d, trying next", instance.id, *instance.currentExec)
		instance.lastExec = *instance.currentExec
		if instance.lastExec%instance.K == 0 {
			instance.Checkpoint(instance.lastExec, instance.consumer.getState())
		}

	} else {
		// XXX This masks a bug, this should not be called when currentExec is nil
		logger.Warn("Replica %d had execDoneSync called, flagging ourselves as out of date", instance.id)
		instance.skipInProgress = true
	}
	instance.currentExec = nil

	instance.executeOutstanding()
}

func (instance *pbftCore) Checkpoint(seqNo uint64, id []byte) {
	if seqNo%instance.K != 0 {
		logger.Error("Attempted to checkpoint a sequence number (%d) which is not a multiple of the checkpoint interval (%d)", seqNo, instance.K)
		return
	}

	idAsString := base64.StdEncoding.EncodeToString(id)

	logger.Debug("Replica %d preparing checkpoint for view=%d/seqNo=%d and b64 id of %s",
		instance.id, instance.view, seqNo, idAsString)

	chkpt := &Checkpoint{
		SequenceNumber: seqNo,
		ReplicaId:      instance.id,
		Id:             idAsString,
	}
	instance.chkpts[seqNo] = idAsString

	instance.persistCheckpoint(seqNo, id)
	instance.recvCheckpoint(chkpt)
	instance.innerBroadcast(&Message{Payload: &Message_Checkpoint{Checkpoint: chkpt}})
}

func (instance *pbftCore) persistCheckpoint(seqNo uint64, id []byte) {
	key := fmt.Sprintf("chkpt.%d", seqNo)
	err := instance.consumer.StoreState(key, id)
	if err != nil {
		logger.Warn("Could not persist Checkpoint %s: %s", key, err)
	}
}

func (instance *pbftCore) recvCheckpoint(chkpt *Checkpoint) tools.Event {
	logger.Debug("Replica %d received checkpoint from replica %d, seqNo %d, digest %s",
		instance.id, chkpt.ReplicaId, chkpt.SequenceNumber, chkpt.Id)

	if instance.weakCheckpointSetOutOfRange(chkpt) {
		return nil
	}

	if !instance.inW(chkpt.SequenceNumber) {
		if chkpt.SequenceNumber != instance.h && !instance.skipInProgress {
			// It is perfectly normal that we receive checkpoints for the watermark we just raised, as we raise it after 2f+1, leaving f replies left
			logger.Warn("Checkpoint sequence number outside watermarks: seqNo %d, low-mark %d", chkpt.SequenceNumber, instance.h)
		} else {
			logger.Debug("Checkpoint sequence number outside watermarks: seqNo %d, low-mark %d", chkpt.SequenceNumber, instance.h)
		}
		return nil
	}

	instance.checkpointStore[*chkpt] = true

	// Track how many different checkpoint values we have for the seqNo in question
	diffValues := make(map[string]struct{})
	diffValues[chkpt.Id] = struct{}{}

	matching := 0
	for testChkpt := range instance.checkpointStore {
		if testChkpt.SequenceNumber == chkpt.SequenceNumber {
			if testChkpt.Id == chkpt.Id {
				matching++
			} else {
				if _, ok := diffValues[testChkpt.Id]; !ok {
					diffValues[testChkpt.Id] = struct{}{}
				}
			}
		}
	}
	logger.Debug("Replica %d found %d matching checkpoints for seqNo %d, digest %s",
		instance.id, matching, chkpt.SequenceNumber, chkpt.Id)

	// If f+2 different values have been observed, we'll never be able to get a stable cert for this seqNo
	if count := len(diffValues); count > instance.f+1 {
		logger.Panic("Network unable to find stable certificate for seqNo %d (%d different values observed already)",
			chkpt.SequenceNumber, count)
	}

	if matching == instance.f+1 {
		// We have a weak cert
		// If we have generated a checkpoint for this seqNo, make sure we have a match
		if ownChkptID, ok := instance.chkpts[chkpt.SequenceNumber]; ok {
			if ownChkptID != chkpt.Id {
				logger.Panic("Own checkpoint for seqNo %d (%s) different from weak checkpoint certificate (%s)",
					chkpt.SequenceNumber, ownChkptID, chkpt.Id)
			}
		}
		instance.witnessCheckpointWeakCert(chkpt)
	}

	if matching < instance.intersectionQuorum() {
		// We do not have a quorum yet
		return nil
	}

	// It is actually just fine if we do not have this checkpoint
	// and should not trigger a state transfer
	// Imagine we are executing sequence number k-1 and we are slow for some reason
	// then everyone else finishes executing k, and we receive a checkpoint quorum
	// which we will agree with very shortly, but do not move our watermarks until
	// we have reached this checkpoint
	// Note, this is not divergent from the paper, as the paper requires that
	// the quorum certificate must contain 2f+1 messages, including its own
	if _, ok := instance.chkpts[chkpt.SequenceNumber]; !ok {
		logger.Debug("Replica %d found checkpoint quorum for seqNo %d, digest %s, but it has not reached this checkpoint itself yet",
			instance.id, chkpt.SequenceNumber, chkpt.Id)
		if instance.skipInProgress {
			logSafetyBound := instance.h + instance.L/2
			// As an optimization, if we are more than half way out of our log and in state transfer, move our watermarks so we don't lose track of the network
			// if needed, state transfer will restart on completion to a more recent point in time
			if chkpt.SequenceNumber >= logSafetyBound {
				logger.Debug("Replica %d is in state transfer, but, the network seems to be moving on past %d, moving our watermarks to stay with it", instance.id, logSafetyBound)
				instance.moveWatermarks(chkpt.SequenceNumber)
			}
		}
		return nil
	}

	logger.Debug("Replica %d found checkpoint quorum for seqNo %d, digest %s",
		instance.id, chkpt.SequenceNumber, chkpt.Id)

	instance.moveWatermarks(chkpt.SequenceNumber)

	return instance.processNewView()
}

func (instance *pbftCore) weakCheckpointSetOutOfRange(chkpt *Checkpoint) bool {
	H := instance.h + instance.L

	// Track the last observed checkpoint sequence number if it exceeds our high watermark, keyed by replica to prevent unbounded growth
	if chkpt.SequenceNumber < H {
		// For non-byzantine nodes, the checkpoint sequence number increases monotonically
		delete(instance.hChkpts, chkpt.ReplicaId)
	} else {
		// We do not track the highest one, as a byzantine node could pick an arbitrarilly high sequence number
		// and even if it recovered to be non-byzantine, we would still believe it to be far ahead
		instance.hChkpts[chkpt.ReplicaId] = chkpt.SequenceNumber

		// If f+1 other replicas have reported checkpoints that were (at one time) outside our watermarks
		// we need to check to see if we have fallen behind.
		if len(instance.hChkpts) >= instance.f+1 {
			chkptSeqNumArray := make([]uint64, len(instance.hChkpts))
			index := 0
			for replicaID, hChkpt := range instance.hChkpts {
				chkptSeqNumArray[index] = hChkpt
				index++
				if hChkpt < H {
					delete(instance.hChkpts, replicaID)
				}
			}
			sort.Sort(sortableUint64Slice(chkptSeqNumArray))

			// If f+1 nodes have issued checkpoints above our high water mark, then
			// we will never record 2f+1 checkpoints for that sequence number, we are out of date
			// (This is because all_replicas - missed - me = 3f+1 - f - 1 = 2f)
			if m := chkptSeqNumArray[len(chkptSeqNumArray)-(instance.f+1)]; m > H {
				logger.Warn("Replica %d is out of date, f+1 nodes agree checkpoint with seqNo %d exists but our high water mark is %d", instance.id, chkpt.SequenceNumber, H)
				instance.reqBatchStore = make(map[string]*RequestBatch) // Discard all our requests, as we will never know which were executed, to be addressed in #394
				instance.persistDelAllRequestBatches()
				instance.moveWatermarks(m)
				instance.outstandingReqBatches = make(map[string]*RequestBatch)
				instance.skipInProgress = true
				instance.consumer.invalidateState()
				instance.stopTimer()

				// TODO, reprocess the already gathered checkpoints, this will make recovery faster, though it is presently correct

				return true
			}
		}
	}

	return false
}

func (instance *pbftCore) moveWatermarks(n uint64) {
	// round down n to previous low watermark
	h := n / instance.K * instance.K

	for idx, cert := range instance.certStore {
		if idx.n <= h {
			logger.Debug("Replica %d cleaning quorum certificate for view=%d/seqNo=%d",
				instance.id, idx.v, idx.n)
			instance.persistDelRequestBatch(cert.digest)
			delete(instance.reqBatchStore, cert.digest)
			delete(instance.certStore, idx)
		}
	}

	for testChkpt := range instance.checkpointStore {
		if testChkpt.SequenceNumber <= h {
			logger.Debug("Replica %d cleaning checkpoint message from replica %d, seqNo %d, b64 snapshot id %s",
				instance.id, testChkpt.ReplicaId, testChkpt.SequenceNumber, testChkpt.Id)
			delete(instance.checkpointStore, testChkpt)
		}
	}

	for n := range instance.pset {
		if n <= h {
			delete(instance.pset, n)
		}
	}

	for idx := range instance.qset {
		if idx.n <= h {
			delete(instance.qset, idx)
		}
	}

	for n := range instance.chkpts {
		if n < h {
			delete(instance.chkpts, n)
			instance.persistDelCheckpoint(n)
		}
	}

	instance.h = h

	logger.Debug("Replica %d updated low watermark to %d",
		instance.id, instance.h)

	instance.resubmitRequestBatches()
}

func (instance *pbftCore) persistDelRequestBatch(digest string) {
	instance.consumer.DelState("reqBatch." + digest)
}

func (instance *pbftCore) persistDelCheckpoint(seqNo uint64) {
	key := fmt.Sprintf("chkpt.%d", seqNo)
	instance.consumer.DelState(key)
}

func (instance *pbftCore) resubmitRequestBatches() {
	if instance.primary(instance.view) != instance.id {
		return
	}

	var submissionOrder []*RequestBatch

outer:
	for d, reqBatch := range instance.outstandingReqBatches {
		for _, cert := range instance.certStore {
			if cert.digest == d {
				logger.Debug("Replica %d already has certificate for request batch %s - not going to resubmit", instance.id, d)
				continue outer
			}
		}
		logger.Debug("Replica %d has detected request batch %s must be resubmitted", instance.id, d)
		submissionOrder = append(submissionOrder, reqBatch)
	}

	if len(submissionOrder) == 0 {
		return
	}

	for _, reqBatch := range submissionOrder {
		// This is a request batch that has not been pre-prepared yet
		// Trigger request batch processing again
		instance.recvRequestBatch(reqBatch)
	}
}

func (instance *pbftCore) witnessCheckpointWeakCert(chkpt *Checkpoint) {
	checkpointMembers := make([]uint64, instance.f+1) // Only ever invoked for the first weak cert, so guaranteed to be f+1
	i := 0
	for testChkpt := range instance.checkpointStore {
		if testChkpt.SequenceNumber == chkpt.SequenceNumber && testChkpt.Id == chkpt.Id {
			checkpointMembers[i] = testChkpt.ReplicaId
			logger.Debug("Replica %d adding replica %d (handle %v) to weak cert", instance.id, testChkpt.ReplicaId, checkpointMembers[i])
			i++
		}
	}

	snapshotID, err := base64.StdEncoding.DecodeString(chkpt.Id)
	if nil != err {
		err = fmt.Errorf("replica %d received a weak checkpoint cert which could not be decoded (%s)", instance.id, chkpt.Id)
		logger.Error(err.Error())
		return
	}

	target := &stateUpdateTarget{
		checkpointMessage: checkpointMessage{
			seqNo: chkpt.SequenceNumber,
			id:    snapshotID,
		},
		replicas: checkpointMembers,
	}
	instance.updateHighStateTarget(target)

	if instance.skipInProgress {
		logger.Debug("Replica %d is catching up and witnessed a weak certificate for checkpoint %d, weak cert attested to by %d of %d (%v)",
			instance.id, chkpt.SequenceNumber, i, instance.replicaCount, checkpointMembers)
		// The view should not be set to active, this should be handled by the yet unimplemented SUSPECT, see https://github.com/hyperledger/fabric/issues/1120
		instance.retryStateTransfer(target)
	}
}

// Delete all BatchRequest
func (instance *pbftCore) persistDelAllRequestBatches() {
	reqBatches, err := instance.consumer.ReadStateSet("reqBatch.")
	if err == nil {
		for k := range reqBatches {
			instance.consumer.DelState(k)
		}
	}
}

// Deal with NewView message
func (instance *pbftCore) processNewView() tools.Event {
	var newReqBatchMissing bool
	nv, ok := instance.newViewStore[instance.view]
	if !ok {
		logger.Debug("Replica %d ignoring processNewView as it could not find view %d in its newViewStore", instance.id, instance.view)
		return nil
	}

	if instance.activeView {
		logger.Info("Replica %d ignoring new-view from %d, v:%d: we are active in view %d",
			instance.id, nv.ReplicaId, nv.View, instance.view)
		return nil
	}

	cp, ok, replicas := instance.selectInitialCheckpoint(nv.Vset)
	if !ok {
		logger.Warn("Replica %d could not determine initial checkpoint: %+v",
			instance.id, instance.viewChangeStore)
		return instance.sendViewChange()
	}

	speculativeLastExec := instance.lastExec
	if instance.currentExec != nil {
		speculativeLastExec = *instance.currentExec
	}

	// If we have not reached the sequence number, check to see if we can reach it without state transfer
	// In general, executions are better than state transfer
	if speculativeLastExec < cp.SequenceNumber {
		canExecuteToTarget := true
	outer:
		for seqNo := speculativeLastExec + 1; seqNo <= cp.SequenceNumber; seqNo++ {
			found := false
			for idx, cert := range instance.certStore {
				if idx.n != seqNo {
					continue
				}

				quorum := 0
				for _, p := range cert.commit {
					// Was this committed in the previous view
					if p.View == idx.v && p.SequenceNumber == seqNo {
						quorum++
					}
				}

				if quorum < instance.intersectionQuorum() {
					logger.Debug("Replica %d missing quorum of commit certificate for seqNo=%d, only has %d of %d", instance.id, quorum, instance.intersectionQuorum())
					continue
				}

				found = true
				break
			}

			if !found {
				canExecuteToTarget = false
				logger.Debug("Replica %d missing commit certificate for seqNo=%d", instance.id, seqNo)
				break outer
			}

		}

		if canExecuteToTarget {
			logger.Debug("Replica %d needs to process a new view, but can execute to the checkpoint seqNo %d, delaying processing of new view", instance.id, cp.SequenceNumber)
			return nil
		}

		logger.Info("Replica %d cannot execute to the view change checkpoint with seqNo %d", instance.id, cp.SequenceNumber)
	}

	msgList := instance.assignSequenceNumbers(nv.Vset, cp.SequenceNumber)
	if msgList == nil {
		logger.Warn("Replica %d could not assign sequence numbers: %+v",
			instance.id, instance.viewChangeStore)
		return instance.sendViewChange()
	}

	if !(len(msgList) == 0 && len(nv.Xset) == 0) && !reflect.DeepEqual(msgList, nv.Xset) {
		logger.Warn("Replica %d failed to verify new-view Xset: computed %+v, received %+v",
			instance.id, msgList, nv.Xset)
		return instance.sendViewChange()
	}

	if instance.h < cp.SequenceNumber {
		instance.moveWatermarks(cp.SequenceNumber)
	}

	if speculativeLastExec < cp.SequenceNumber {
		logger.Warn("Replica %d missing base checkpoint %d (%s), our most recent execution %d", instance.id, cp.SequenceNumber, cp.Id, speculativeLastExec)

		snapshotID, err := base64.StdEncoding.DecodeString(cp.Id)
		if nil != err {
			err = fmt.Errorf("replica %d received a view change whose hash could not be decoded (%s)", instance.id, cp.Id)
			logger.Error(err.Error())
			return nil
		}

		target := &stateUpdateTarget{
			checkpointMessage: checkpointMessage{
				seqNo: cp.SequenceNumber,
				id:    snapshotID,
			},
			replicas: replicas,
		}

		instance.updateHighStateTarget(target)
		instance.stateTransfer(target)
	}

	for n, d := range nv.Xset {
		// PBFT: why should we use "h ≥ min{n | ∃d : (<n,d> ∈ X)}"?
		// "h ≥ min{n | ∃d : (<n,d> ∈ X)} ∧ ∀<n,d> ∈ X : (n ≤ h ∨ ∃m ∈ in : (D(m) = d))"
		if n <= instance.h {
			continue
		} else {
			if d == "" {
				// NULL request; skip
				continue
			}

			if _, ok := instance.reqBatchStore[d]; !ok {
				logger.Warn("Replica %d missing assigned, non-checkpointed request batch %s",
					instance.id, d)
				if _, ok := instance.missingReqBatches[d]; !ok {
					logger.Warn("Replica %v requesting to fetch batch %s",
						instance.id, d)
					newReqBatchMissing = true
					instance.missingReqBatches[d] = true
				}
			}
		}
	}

	if len(instance.missingReqBatches) == 0 {
		return instance.processNewView2(nv)
	} else if newReqBatchMissing {
		instance.fetchRequestBatches()
	}

	return nil
}

func (instance *pbftCore) selectInitialCheckpoint(vset []*ViewChange) (checkpoint ViewChange_C, ok bool, replicas []uint64) {
	checkpoints := make(map[ViewChange_C][]*ViewChange)
	for _, vc := range vset {
		for _, c := range vc.Cset { // TODO, verify that we strip duplicate checkpoints from this set
			checkpoints[*c] = append(checkpoints[*c], vc)
			logger.Debug("Replica %d appending checkpoint from replica %d with seqNo=%d, h=%d, and checkpoint digest %s", instance.id, vc.ReplicaId, vc.H, c.SequenceNumber, c.Id)
		}
	}

	if len(checkpoints) == 0 {
		logger.Debug("Replica %d has no checkpoints to select from: %d %s",
			instance.id, len(instance.viewChangeStore), checkpoints)
		return
	}

	for idx, vcList := range checkpoints {
		// need weak certificate for the checkpoint
		if len(vcList) <= instance.f { // type casting necessary to match types
			logger.Debug("Replica %d has no weak certificate for n:%d, vcList was %d long",
				instance.id, idx.SequenceNumber, len(vcList))
			continue
		}

		quorum := 0
		// Note, this is the whole vset (S) in the paper, not just this checkpoint set (S') (vcList)
		// We need 2f+1 low watermarks from S below this seqNo from all replicas
		// We need f+1 matching checkpoints at this seqNo (S')
		for _, vc := range vset {
			if vc.H <= idx.SequenceNumber {
				quorum++
			}
		}

		if quorum < instance.intersectionQuorum() {
			logger.Debug("Replica %d has no quorum for n:%d", instance.id, idx.SequenceNumber)
			continue
		}

		replicas = make([]uint64, len(vcList))
		for i, vc := range vcList {
			replicas[i] = vc.ReplicaId
		}

		if checkpoint.SequenceNumber <= idx.SequenceNumber {
			checkpoint = idx
			ok = true
		}
	}
	return
}

func (instance *pbftCore) updateHighStateTarget(target *stateUpdateTarget) {
	if instance.highStateTarget != nil && instance.highStateTarget.seqNo >= target.seqNo {
		logger.Debug("Replica %d not updating state target to seqNo %d, has target for seqNo %d", instance.id, target.seqNo, instance.highStateTarget.seqNo)
		return
	}

	instance.highStateTarget = target
}

// used in view-change to fetch missing assigned, non-checkpointed requests
func (instance *pbftCore) fetchRequestBatches() (err error) {
	var msg *Message
	for digest := range instance.missingReqBatches {
		msg = &Message{Payload: &Message_FetchRequestBatch{FetchRequestBatch: &FetchRequestBatch{
			BatchDigest: digest,
			ReplicaId:   instance.id,
		}}}
		instance.innerBroadcast(msg)
	}

	return
}

func (instance *pbftCore) stateTransfer(optional *stateUpdateTarget) {
	if !instance.skipInProgress {
		logger.Debug("Replica %d is out of sync, pending state transfer", instance.id)
		instance.skipInProgress = true
		instance.consumer.invalidateState()
	}

	instance.retryStateTransfer(optional)
}

func (instance *pbftCore) retryStateTransfer(optional *stateUpdateTarget) {
	if instance.currentExec != nil {
		logger.Debug("Replica %d is currently mid-execution, it must wait for the execution to complete before performing state transfer", instance.id)
		return
	}

	if instance.stateTransferring {
		logger.Debug("Replica %d is currently mid state transfer, it must wait for this state transfer to complete before initiating a new one", instance.id)
		return
	}

	target := optional
	if target == nil {
		if instance.highStateTarget == nil {
			logger.Debug("Replica %d has no targets to attempt state transfer to, delaying", instance.id)
			return
		}
		target = instance.highStateTarget
	}

	instance.stateTransferring = true

	logger.Debug("Replica %d is initiating state transfer to seqNo %d", instance.id, target.seqNo)
	instance.consumer.skipTo(target.seqNo, target.id, target.replicas)

}

func (instance *pbftCore) processNewView2(nv *NewView) tools.Event {
	logger.Info("Replica %d accepting new-view to view %d", instance.id, instance.view)

	instance.stopTimer()
	instance.nullRequestTimer.Stop()

	instance.activeView = true
	delete(instance.newViewStore, instance.view-1)

	instance.seqNo = instance.h
	for n, d := range nv.Xset {
		if n <= instance.h {
			continue
		}

		reqBatch, ok := instance.reqBatchStore[d]
		if !ok && d != "" {
			logger.Fatal("Replica %d is missing request batch for seqNo=%d with digest '%s' for assigned prepare after fetching, this indicates a serious bug", instance.id, n, d)
		}
		preprep := &PrePrepare{
			View:           instance.view,
			SequenceNumber: n,
			BatchDigest:    d,
			RequestBatch:   reqBatch,
			ReplicaId:      instance.id,
		}
		cert := instance.getCert(instance.view, n)
		cert.prePrepare = preprep
		cert.digest = d
		if n > instance.seqNo {
			instance.seqNo = n
		}
		instance.persistQSet()
	}

	instance.updateViewChangeSeqNo()

	if instance.primary(instance.view) != instance.id {
		for n, d := range nv.Xset {
			prep := &Prepare{
				View:           instance.view,
				SequenceNumber: n,
				BatchDigest:    d,
				ReplicaId:      instance.id,
			}
			if n > instance.h {
				cert := instance.getCert(instance.view, n)
				cert.sentPrepare = true
				instance.recvPrepare(prep)
			}
			instance.innerBroadcast(&Message{Payload: &Message_Prepare{Prepare: prep}})
		}
	} else {
		logger.Debug("Replica %d is now primary, attempting to resubmit requests", instance.id)
		instance.resubmitRequestBatches()
	}

	instance.startTimerIfOutstandingRequests()

	logger.Debug("Replica %d done cleaning view change artifacts, calling into consumer", instance.id)

	return viewChangedEvent{}
}

func (instance *pbftCore) updateViewChangeSeqNo() {
	if instance.viewChangePeriod <= 0 {
		return
	}
	// Ensure the view change always occurs at a checkpoint boundary
	instance.viewChangeSeqNo = instance.seqNo + instance.viewChangePeriod*instance.K - instance.seqNo%instance.K
	logger.Debug("Replica %d updating view change sequence number to %d", instance.id, instance.viewChangeSeqNo)
}

// =============================================================================
// State reserve for PBFT
// =============================================================================

// Given a digest/view/seq, is there an entry in the certLog ?
// If so, return it. If not, create it.
func (instance *pbftCore) getCert(v uint64, n uint64) (cert *msgCert) {
	idx := msgID{v, n}
	cert, ok := instance.certStore[idx]
	if ok {
		return
	}

	cert = &msgCert{}
	instance.certStore[idx] = cert
	return
}

func (instance *pbftCore) persistQSet() {
	var qset []*ViewChange_PQ

	for _, q := range instance.calcQSet() {
		qset = append(qset, q)
	}

	instance.persistPQSet("qset", qset)
}

func (instance *pbftCore) persistPSet() {
	var pset []*ViewChange_PQ

	for _, p := range instance.calcPSet() {
		pset = append(pset, p)
	}

	instance.persistPQSet("pset", pset)
}

func (instance *pbftCore) persistPQSet(key string, set []*ViewChange_PQ) {
	raw, err := proto.Marshal(&PQset{Set: set})
	if err != nil {
		logger.Warn("Replica %d could not persist pqset: %s: error: %s", instance.id, key, err)
		return
	}
	err = instance.consumer.StoreState(key, raw)
	if err != nil {
		logger.Warn("Replica %d could not persist pqset: %s: error: %s", instance.id, key, err)
	}
}

func (instance *pbftCore) calcPSet() map[uint64]*ViewChange_PQ {
	pset := make(map[uint64]*ViewChange_PQ)

	for n, p := range instance.pset {
		pset[n] = p
	}

	// P set: requests that have prepared here
	//
	// "<n,d,v> has a prepared certificate, and no request
	// prepared in a later view with the same number"

	for idx, cert := range instance.certStore {
		if cert.prePrepare == nil {
			continue
		}

		digest := cert.digest
		if !instance.prepared(digest, idx.v, idx.n) {
			continue
		}

		if p, ok := pset[idx.n]; ok && p.View > idx.v {
			continue
		}

		pset[idx.n] = &ViewChange_PQ{
			SequenceNumber: idx.n,
			BatchDigest:    digest,
			View:           idx.v,
		}
	}

	return pset
}

func (instance *pbftCore) calcQSet() map[qidx]*ViewChange_PQ {
	qset := make(map[qidx]*ViewChange_PQ)

	for n, q := range instance.qset {
		qset[n] = q
	}

	// Q set: requests that have pre-prepared here (pre-prepare or
	// prepare sent)
	//
	// "<n,d,v>: requests that pre-prepared here, and did not
	// pre-prepare in a later view with the same number"

	for idx, cert := range instance.certStore {
		if cert.prePrepare == nil {
			continue
		}

		digest := cert.digest
		if !instance.prePrepared(digest, idx.v, idx.n) {
			continue
		}

		qi := qidx{digest, idx.n}
		if q, ok := qset[qi]; ok && q.View > idx.v {
			continue
		}

		qset[qi] = &ViewChange_PQ{
			SequenceNumber: idx.n,
			BatchDigest:    digest,
			View:           idx.v,
		}
	}

	return qset
}

// =============================================================================
// send methods
// =============================================================================

// Send Pre-Prepare message
func (instance *pbftCore) sendPrePrepare(reqBatch *RequestBatch, digest string) {
	logger.Debug("Replica %d is primary, issuing pre-prepare for request batch %s", instance.id, digest)

	n := instance.seqNo + 1
	for _, cert := range instance.certStore { // check for other PRE-PREPARE for same digest, but different seqNo
		if p := cert.prePrepare; p != nil {
			if p.View == instance.view && p.SequenceNumber != n && p.BatchDigest == digest && digest != "" {
				logger.Info("Other pre-prepare found with same digest but different seqNo: %d instead of %d", p.SequenceNumber, n)
				return
			}
		}
	}

	if !instance.inWV(instance.view, n) || n > instance.h+instance.L/2 {
		// We don't have the necessary stable certificates to advance our watermarks
		logger.Warn("Primary %d not sending pre-prepare for batch %s - out of sequence numbers", instance.id, digest)
		return
	}

	if n > instance.viewChangeSeqNo {
		logger.Info("Primary %d about to switch to next primary, not sending pre-prepare with seqno=%d", instance.id, n)
		return
	}

	logger.Debug("Primary %d broadcasting pre-prepare for view=%d/seqNo=%d and digest %s", instance.id, instance.view, n, digest)
	instance.seqNo = n
	preprep := &PrePrepare{
		View:           instance.view,
		SequenceNumber: n,
		BatchDigest:    digest,
		RequestBatch:   reqBatch,
		ReplicaId:      instance.id,
	}
	cert := instance.getCert(instance.view, n)
	cert.prePrepare = preprep
	cert.digest = digest
	instance.persistQSet()
	instance.innerBroadcast(&Message{Payload: &Message_PrePrepare{PrePrepare: preprep}})
	instance.maybeSendCommit(digest, instance.view, n)
}

// May be send commit message
func (instance *pbftCore) maybeSendCommit(digest string, v uint64, n uint64) error {
	cert := instance.getCert(v, n)
	if instance.prepared(digest, v, n) && !cert.sentCommit {
		logger.Debug("Replica %d broadcasting commit for view=%d/seqNo=%d",
			instance.id, v, n)
		commit := &Commit{
			View:           v,
			SequenceNumber: n,
			BatchDigest:    digest,
			ReplicaId:      instance.id,
		}
		cert.sentCommit = true
		instance.recvCommit(commit)
		return instance.innerBroadcast(&Message{Payload: &Message_Commit{Commit: commit}})
	}
	return nil
}

// View-Change message
func (instance *pbftCore) sendViewChange() tools.Event {
	instance.stopTimer()

	delete(instance.newViewStore, instance.view)
	instance.view++
	instance.activeView = false

	instance.pset = instance.calcPSet()
	instance.qset = instance.calcQSet()

	// clear old messages
	for idx := range instance.certStore {
		if idx.v < instance.view {
			delete(instance.certStore, idx)
		}
	}
	for idx := range instance.viewChangeStore {
		if idx.v < instance.view {
			delete(instance.viewChangeStore, idx)
		}
	}

	vc := &ViewChange{
		View:      instance.view,
		H:         instance.h,
		ReplicaId: instance.id,
	}

	for n, id := range instance.chkpts {
		vc.Cset = append(vc.Cset, &ViewChange_C{
			SequenceNumber: n,
			Id:             id,
		})
	}

	for _, p := range instance.pset {
		if p.SequenceNumber < instance.h {
			logger.Error("BUG! Replica %d should not have anything in our pset less than h, found %+v", instance.id, p)
		}
		vc.Pset = append(vc.Pset, p)
	}

	for _, q := range instance.qset {
		if q.SequenceNumber < instance.h {
			logger.Error("BUG! Replica %d should not have anything in our qset less than h, found %+v", instance.id, q)
		}
		vc.Qset = append(vc.Qset, q)
	}

	instance.sign(vc)

	logger.Info("Replica %d sending view-change, v:%d, h:%d, |C|:%d, |P|:%d, |Q|:%d",
		instance.id, vc.View, vc.H, len(vc.Cset), len(vc.Pset), len(vc.Qset))

	instance.innerBroadcast(&Message{Payload: &Message_ViewChange{ViewChange: vc}})

	instance.vcResendTimer.Reset(instance.vcResendTimeout, viewChangeResendTimerEvent{})

	return instance.recvViewChange(vc)
}

// =============================================================================
// pre-prepare/prepare/commit quorum checks
// =============================================================================

// intersectionQuorum returns the number of replicas that have to
// agree to guarantee that at least one correct replica is shared by
// two intersection quora
func (instance *pbftCore) intersectionQuorum() int {
	return (instance.N + instance.f + 2) / 2
}

// allCorrectReplicasQuorum returns the number of correct replicas (N-f)
func (instance *pbftCore) allCorrectReplicasQuorum() int {
	return instance.N - instance.f
}

// Whether prepare phase has passed
func (instance *pbftCore) prepared(digest string, v uint64, n uint64) bool {
	if !instance.prePrepared(digest, v, n) {
		return false
	}

	if p, ok := instance.pset[n]; ok && p.View == v && p.BatchDigest == digest {
		return true
	}

	quorum := 0
	cert := instance.certStore[msgID{v, n}]
	if cert == nil {
		return false
	}

	for _, p := range cert.prepare {
		if p.View == v && p.SequenceNumber == n && p.BatchDigest == digest {
			quorum++
		}
	}

	logger.Debug("Replica %d prepare count for view=%d/seqNo=%d: %d", instance.id, v, n, quorum)

	return quorum >= instance.intersectionQuorum()-1
}

// Whether pre-prepare phase has passed
func (instance *pbftCore) prePrepared(digest string, v uint64, n uint64) bool {
	_, mInLog := instance.reqBatchStore[digest]

	if digest != "" && !mInLog {
		return false
	}

	if q, ok := instance.qset[qidx{digest, n}]; ok && q.View == v {
		return true
	}

	cert := instance.certStore[msgID{v, n}]
	if cert != nil {
		p := cert.prePrepare
		if p != nil && p.View == v && p.SequenceNumber == n && p.BatchDigest == digest {
			return true
		}
	}
	logger.Debug("Replica %d does not have view=%d/seqNo=%d pre-prepared", instance.id, v, n)
	return false
}

func (instance *pbftCore) committed(digest string, v uint64, n uint64) bool {
	if !instance.prepared(digest, v, n) {
		return false
	}

	quorum := 0
	cert := instance.certStore[msgID{v, n}]
	if cert == nil {
		return false
	}

	for _, p := range cert.commit {
		if p.View == v && p.SequenceNumber == n {
			quorum++
		}
	}

	logger.Debug("Replica %d commit count for view=%d/seqNo=%d: %d",
		instance.id, v, n, quorum)

	return quorum >= instance.intersectionQuorum()
}

// =============================================================================
// helper functions for PBFT
// =============================================================================

// Given a certain view n, what is the expected primary?
func (instance *pbftCore) primary(n uint64) uint64 {
	return n % uint64(instance.replicaCount)
}

// Is the view right? And is the sequence number between watermarks?
func (instance *pbftCore) inWV(v uint64, n uint64) bool {
	return instance.view == v && instance.inW(n)
}

// Is the sequence number between watermarks?
func (instance *pbftCore) inW(n uint64) bool {
	return n-instance.h > 0 && n-instance.h <= instance.L
}

// =============================================================================
// Misc. methods go here
// =============================================================================

func (instance *pbftCore) startTimerIfOutstandingRequests() {
	if instance.skipInProgress || instance.currentExec != nil {
		// Do not start the view change timer if we are executing or state transferring, these take arbitrarilly long amounts of time
		return
	}

	if len(instance.outstandingReqBatches) > 0 {
		getOutstandingDigests := func() []string {
			var digests []string
			for digest := range instance.outstandingReqBatches {
				digests = append(digests, digest)
			}
			return digests
		}()
		instance.softStartTimer(instance.requestTimeout, fmt.Sprintf("outstanding request batches %v", getOutstandingDigests))
	} else if instance.nullRequestTimeout > 0 {
		timeout := instance.nullRequestTimeout
		if instance.primary(instance.view) != instance.id {
			// we're waiting for the primary to deliver a null request - give it a bit more time
			timeout += instance.requestTimeout
		}
		instance.nullRequestTimer.Reset(timeout, nullRequestEvent{})
	}
}

func (instance *pbftCore) softStartTimer(timeout time.Duration, reason string) {
	logger.Debug("Replica %d soft starting new view timer for %s: %s", instance.id, timeout, reason)
	instance.newViewTimerReason = reason
	instance.timerActive = true
	instance.newViewTimer.SoftReset(timeout, viewChangeTimerEvent{})
}

func (instance *pbftCore) startTimer(timeout time.Duration, reason string) {
	logger.Debug("Replica %d starting new view timer for %s: %s", instance.id, timeout, reason)
	instance.timerActive = true
	instance.newViewTimer.Reset(timeout, viewChangeTimerEvent{})
}

func (instance *pbftCore) stopTimer() {
	logger.Debug("Replica %d stopping a running new view timer", instance.id)
	instance.timerActive = false
	instance.newViewTimer.Stop()
}

// Marshals a Message and hands it to the Stack. If toSelf is true,
// the message is also dispatched to the local instance's RecvMsgSync.
func (instance *pbftCore) innerBroadcast(msg *Message) error {
	msgRaw, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("cannot marshal message %s", err)
	}

	doByzantine := false
	if instance.byzantine {
		rand1 := rand.New(rand.NewSource(time.Now().UnixNano()))
		doIt := rand1.Intn(3) // go byzantine about 1/3 of the time
		if doIt == 1 {
			doByzantine = true
		}
	}

	// testing byzantine fault.
	if doByzantine {
		rand2 := rand.New(rand.NewSource(time.Now().UnixNano()))
		ignoreidx := rand2.Intn(instance.N)
		for i := 0; i < instance.N; i++ {
			if i != ignoreidx && uint64(i) != instance.id { //Pick a random replica and do not send message
				instance.consumer.unicast(msgRaw, uint64(i))
			} else {
				logger.Debug("PBFT byzantine: not broadcasting to replica %v", i)
			}
		}
	} else {
		instance.consumer.broadcast(msgRaw)
	}
	return nil
}

package pbft

import (
	logger "github.com/DSiSc/craft/log"
	"github.com/DSiSc/spree/pbft/common"
)

func (instance *pbftCore) correctViewChange(vc *common.ViewChange) bool {
	for _, p := range append(vc.Pset, vc.Qset...) {
		if !(p.View < vc.View && p.SequenceNumber > vc.H && p.SequenceNumber <= vc.H+instance.L) {
			logger.Debug("Replica %d invalid p entry in view-change: vc(v:%d h:%d) p(v:%d n:%d)",
				instance.id, vc.View, vc.H, p.View, p.SequenceNumber)
			return false
		}
	}

	for _, c := range vc.Cset {
		// PBFT: the paper says c.n > vc.h
		if !(c.SequenceNumber >= vc.H && c.SequenceNumber <= vc.H+instance.L) {
			logger.Debug("Replica %d invalid c entry in view-change: vc(v:%d h:%d) c(n:%d)",
				instance.id, vc.View, vc.H, c.SequenceNumber)
			return false
		}
	}

	return true
}

func (instance *pbftCore) assignSequenceNumbers(vset []*common.ViewChange, h uint64) (msgList map[uint64]string) {
	msgList = make(map[uint64]string)

	maxN := h + 1

	// "for all n such that h < n <= h + L"
nLoop:
	for n := h + 1; n <= h+instance.L; n++ {
		// "∃m ∈ S..."
		for _, m := range vset {
			// "...with <n,d,v> ∈ m.P"
			for _, em := range m.Pset {
				quorum := 0
				// "A1. ∃2f+1 messages m' ∈ S"
			mpLoop:
				for _, mp := range vset {
					if mp.H >= n {
						continue
					}
					// "∀<n,d',v'> ∈ m'.P"
					for _, emp := range mp.Pset {
						if n == emp.SequenceNumber && !(emp.View < em.View || (emp.View == em.View && emp.BatchDigest == em.BatchDigest)) {
							continue mpLoop
						}
					}
					quorum++
				}

				if quorum < instance.intersectionQuorum() {
					continue
				}

				quorum = 0
				// "A2. ∃f+1 messages m' ∈ S"
				for _, mp := range vset {
					// "∃<n,d',v'> ∈ m'.Q"
					for _, emp := range mp.Qset {
						if n == emp.SequenceNumber && emp.View >= em.View && emp.BatchDigest == em.BatchDigest {
							quorum++
						}
					}
				}

				if quorum < instance.f+1 {
					continue
				}

				// "then select the request with digest d for number n"
				msgList[n] = em.BatchDigest
				maxN = n

				continue nLoop
			}
		}

		quorum := 0
		// "else if ∃2f+1 messages m ∈ S"
	nullLoop:
		for _, m := range vset {
			// "m.P has no entry"
			for _, em := range m.Pset {
				if em.SequenceNumber == n {
					continue nullLoop
				}
			}
			quorum++
		}

		if quorum >= instance.intersectionQuorum() {
			// "then select the null request for number n"
			msgList[n] = ""

			continue nLoop
		}

		logger.Warn("Replica %d could not assign value to contents of seqNo %d, found only %d missing P entries", instance.id, n, quorum)
		return nil
	}

	// prune top null requests
	for n, msg := range msgList {
		if n > maxN && msg == "" {
			delete(msgList, n)
		}
	}

	return
}

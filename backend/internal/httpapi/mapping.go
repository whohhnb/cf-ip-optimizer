package httpapi

import "edgepilot/internal/store"

func storeDomainInput(in domainInput) store.DomainInput {
	return store.DomainInput{
		AccountID:   in.AccountID,
		RootDomain:  in.RootDomain,
		SubDomain:   in.SubDomain,
		RecordLine:  in.RecordLine,
		ProbeID:     in.ProbeID,
		TTL:         in.TTL,
		Slots:       in.Slots,
		AutoPush:    in.AutoPush,
		IntervalMin: in.IntervalMin,
		Enabled:     in.Enabled,
	}
}

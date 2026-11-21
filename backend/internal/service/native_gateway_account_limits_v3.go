package service

import (
	"context"
	"math"
	"time"
)

const GatewayAccountLimitsContractV3 = "account-limits/v3"

// V3 opens provider identity and retains the same observed dimension, unit and
// period vocabulary. No limit is inferred from a method, plan or API-key kind.
type GatewayAccountLimitsResultV3 = GatewayAccountLimitsResultV2

type GatewayNativeConnectionLimitsCache interface {
	ReadGatewayNativeConnectionLimits(context.Context, GatewayNativeCredentialScope, string, GatewayNativePublishedRoute) (GatewayAccountLimitsStoredV2, error)
}

type GatewayAccountLimitsReaderV3 struct {
	native    *GatewayNativeConnections
	legacy    *GatewayAccountLimitsReader
	legacyV2  *GatewayAccountLimitsReaderV2
	authorize GatewayAccountLimitsAuthorizer
}

func NewGatewayAccountLimitsReaderV3(native *GatewayNativeConnections, legacy *GatewayAccountLimitsReader, legacyV2 *GatewayAccountLimitsReaderV2, authorize GatewayAccountLimitsAuthorizer) (*GatewayAccountLimitsReaderV3, error) {
	if authorize == nil || native == nil && legacy == nil && legacyV2 == nil {
		return nil, ErrGatewayNativeIdentity
	}
	return &GatewayAccountLimitsReaderV3{native, legacy, legacyV2, authorize}, nil
}

func (s *GatewayAccountLimitsReaderV3) Read(ctx context.Context, in GatewayAccountLimitsInput) GatewayAccountLimitsResultV3 {
	unavailable := GatewayAccountLimitsResultV3{Status: "unavailable"}
	if s == nil || ctx == nil || !in.Valid() {
		return unavailable
	}
	before, err := s.authorize(ctx, in)
	if err != nil || ctx.Err() != nil {
		return unavailable
	}
	if s.native != nil {
		bound, marked, err := s.native.BindAccountRead(ctx, in, before)
		if err != nil {
			return unavailable
		}
		if marked {
			after, err := s.authorize(ctx, in)
			if err != nil || !SameGatewayNativeDescriptor(before, after) {
				return unavailable
			}
			result := s.native.ReadLimits(bound, in)
			if !s.native.CheckAccountRead(bound, in, after) {
				return unavailable
			}
			return result
		}
	}
	definition, found := LookupGatewayConnectionProfile(in.ProfileID)
	if !found {
		return unavailable
	}
	switch definition.LimitsContract {
	case GatewayAccountLimitsContract:
		if s.legacy != nil {
			return projectGatewayAccountLimitsV1V3(in.ProfileID, s.legacy.Read(ctx, in))
		}
	case GatewayAccountLimitsContractV2:
		if s.legacyV2 != nil {
			return s.legacyV2.Read(ctx, in)
		}
	case "unsupported":
		return GatewayAccountLimitsResultV3{Status: "unsupported"}
	}
	return unavailable
}

func (s *GatewayNativeConnections) ReadLimits(ctx context.Context, in GatewayAccountLimitsInput) GatewayAccountLimitsResultV3 {
	unavailable := GatewayAccountLimitsResultV3{Status: "unavailable"}
	binding, ok := s.accountReadBinding(ctx, in)
	if !ok {
		return unavailable
	}
	definition, found := LookupGatewayConnectionProfile(in.ProfileID)
	if !found {
		return unavailable
	}
	if definition.LimitsContract == "unsupported" {
		return GatewayAccountLimitsResultV3{Status: "unsupported"}
	}
	cache, ok := s.repository.(GatewayNativeConnectionLimitsCache)
	if !ok {
		return unavailable
	}
	p := binding.physical
	stored, err := cache.ReadGatewayNativeConnectionLimits(ctx, p.Scope, p.Operation, p.Native)
	if err != nil || ctx.Err() != nil || stored.PrincipalVersion != GatewayNativeConnectionObservationPrincipal(p) {
		return unavailable
	}
	if stored.Snapshot == nil || len(stored.Snapshot.Windows) == 0 {
		return GatewayAccountLimitsResultV3{Status: "unknown"}
	}
	if stored.Revision < 1 || stored.Revision > gatewayLimitsMaxInteger || !ValidateGatewayAccountLimitsSnapshotV3(*stored.Snapshot, in.ProfileID) {
		return unavailable
	}
	return GatewayAccountLimitsResultV3{Status: "snapshot", PhysicalGeneration: p.Scope.Generation, PrincipalVersion: stored.PrincipalVersion,
		Revision: stored.Revision, Scope: stored.Snapshot.Scope, Windows: stored.Snapshot.Windows}
}

func ValidateGatewayAccountLimitsSnapshotV3(snapshot GatewayAccountLimitsSnapshotV2, profile string) bool {
	definition, found := LookupGatewayConnectionProfile(profile)
	if !found || definition.LimitsContract == "unsupported" || snapshot.Scope != "account" && snapshot.Scope != "subscription" ||
		!ValidateGatewayAccountLimitWindowsV3(snapshot.Windows) {
		return false
	}
	for _, window := range snapshot.Windows {
		if window.Provider != definition.Provider.ID {
			return false
		}
	}
	return true
}

func ValidateGatewayAccountLimitWindowsV3(windows []GatewayAccountLimitWindowV2) bool {
	return validateGatewayAccountLimitWindows(windows, gatewayConnectionSemanticID.MatchString)
}

// V1's ordinary windows can be represented without inventing a unit. Distinct
// Claude model buckets have no equivalent in this schema and are never merged
// into the subscription-wide weekly window; their original v1 remains intact.
func projectGatewayAccountLimitsV1V3(profile string, old GatewayAccountLimitsResult) GatewayAccountLimitsResultV3 {
	if old.Status != "snapshot" {
		return GatewayAccountLimitsResultV3{Status: old.Status}
	}
	definition, found := LookupGatewayConnectionProfile(profile)
	if !found {
		return GatewayAccountLimitsResultV3{Status: "unavailable"}
	}
	windows := make([]GatewayAccountLimitWindowV2, 0, len(old.Windows))
	for _, previous := range old.Windows {
		id := previous.ID
		switch id {
		case GatewayClaudeFiveHour:
			id = "five_hour"
		case GatewayClaudeSevenDay:
			id = "weekly"
		case GatewayClaudeSonnetSevenDay, GatewayClaudeFableSevenDay:
			continue
		}
		window := GatewayAccountLimitWindowV2{ID: id, Provider: definition.Provider.ID, Dimension: "plan_quota", Unit: "unknown",
			Period: GatewayAccountLimitPeriodV2{Kind: "unknown"}, UsedPercent: previous.UsedPercent, ObservedAt: previous.ObservedAt, ResetAt: previous.ResetAt}
		if previous.DurationSeconds != nil && previous.Basis == "duration" {
			window.Period = GatewayAccountLimitPeriodV2{Kind: "duration", DurationSeconds: previous.DurationSeconds}
			label := ""
			if *previous.DurationSeconds == 5*60*60 {
				label = "5h"
			} else if *previous.DurationSeconds == 7*24*60*60 {
				label = "Week"
			}
			if label != "" {
				window.Label = &label
			}
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return GatewayAccountLimitsResultV3{Status: "unknown"}
	}
	if !ValidateGatewayAccountLimitWindowsV3(windows) {
		return GatewayAccountLimitsResultV3{Status: "unavailable"}
	}
	return GatewayAccountLimitsResultV3{Status: "snapshot", PhysicalGeneration: old.PhysicalGeneration, PrincipalVersion: old.PrincipalVersion,
		Revision: old.Revision, Scope: old.Scope, Windows: windows}
}

func validateGatewayAccountLimitWindows(windows []GatewayAccountLimitWindowV2, providerAllowed func(string) bool) bool {
	if len(windows) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, w := range windows {
		switch w.ID {
		case "five_hour", "weekly", "monthly", "primary", "secondary", "unknown", "request_rate", "token_rate":
		default:
			return false
		}
		if !providerAllowed(w.Provider) {
			return false
		}
		identity := w.Provider + "/" + w.ID + "/" + w.Dimension + "/" + w.Unit
		if seen[identity] {
			return false
		}
		seen[identity] = true
		if w.Dimension != "plan_quota" && w.Dimension != "rate_limit" {
			return false
		}
		switch w.Unit {
		case "credits", "tokens", "requests", "unknown":
		default:
			return false
		}
		switch w.Period.Kind {
		case "duration":
			if w.Period.DurationSeconds == nil || *w.Period.DurationSeconds <= 0 || *w.Period.DurationSeconds > gatewayLimitsMaxInteger {
				return false
			}
		case "monthly_billing_cycle", "unknown":
			if w.Period.DurationSeconds != nil {
				return false
			}
		default:
			return false
		}
		if w.Label != nil {
			switch *w.Label {
			case "5h":
				if w.Period.Kind != "duration" || *w.Period.DurationSeconds != 18000 {
					return false
				}
			case "Week":
				if w.Period.Kind != "duration" || *w.Period.DurationSeconds != 604800 {
					return false
				}
			case "Month":
				if w.Period.Kind != "monthly_billing_cycle" {
					return false
				}
			default:
				return false
			}
		}
		if w.UsedPercent != nil && (math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0) {
			return false
		}
		for _, raw := range []*string{w.ObservedAt, w.ResetAt} {
			if raw != nil {
				t, err := time.Parse(time.RFC3339Nano, *raw)
				if err != nil || t.Year() < 1 || t.Year() > 9999 {
					return false
				}
			}
		}
		if w.UsedPercent != nil && w.ObservedAt == nil {
			return false
		}
		// Rate observations cannot masquerade as subscription quota windows.
		if (w.ID == "request_rate" || w.ID == "token_rate") && w.Dimension != "rate_limit" {
			return false
		}
		if w.Dimension == "rate_limit" && w.Label != nil {
			return false
		}
	}
	return true
}

//go:build unit

package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

const connectionInventoryMaxBytes = 2 << 20

type connectionInventoryDelta struct {
	Change    string          `json:"change"`
	ProfileID string          `json:"profileId"`
	MethodID  string          `json:"methodId,omitempty"`
	Scope     string          `json:"scope,omitempty"`
	FieldID   string          `json:"fieldId,omitempty"`
	Before    json.RawMessage `json:"before,omitempty"`
	After     json.RawMessage `json:"after,omitempty"`
}

type connectionInventoryDiff struct {
	Contract          string                     `json:"contract"`
	Baseline          string                     `json:"baseline"`
	BaseRevision      string                     `json:"baseRevision,omitempty"`
	CurrentRevision   string                     `json:"currentRevision,omitempty"`
	Products          []connectionInventoryDelta `json:"products"`
	Methods           []connectionInventoryDelta `json:"methods"`
	Inputs            []connectionInventoryDelta `json:"inputs"`
	Requirements      []connectionInventoryDelta `json:"requirements"`
	SemanticRevisions []connectionInventoryDelta `json:"semanticRevisions"`
}

func validateConnectionInventory(snapshot PlatformNativeConnectionInventory) error {
	if snapshot.Contract != "native-connection-inventory/v1" || len(snapshot.Profiles) > 128 {
		return errors.New("native_inventory_contract_or_profile_bound")
	}
	profiles := map[string]bool{}
	for _, profile := range snapshot.Profiles {
		if !gatewayConnectionSemanticID.MatchString(profile.ProfileID) || profiles[profile.ProfileID] || len(profile.Methods) == 0 || len(profile.Methods) > 32 {
			return errors.New("native_inventory_profile_identity_or_method_bound")
		}
		profiles[profile.ProfileID] = true
		methods := map[string]bool{}
		for _, method := range profile.Methods {
			if !gatewayConnectionSemanticID.MatchString(method.MethodID) || methods[method.MethodID] || method.Revision < 1 {
				return errors.New("native_inventory_method_identity_or_revision")
			}
			methods[method.MethodID] = true
			for _, fields := range [][]GatewayConnectionField{method.Interaction.InputFields, method.Interaction.PreflightFields} {
				if len(fields) > GatewayConnectionInputFields {
					return errors.New("native_inventory_field_bound")
				}
				seen := map[string]bool{}
				for _, field := range fields {
					if !gatewayConnectionSemanticID.MatchString(field.FieldID) || seen[field.FieldID] || field.MaxBytes < 1 || field.MaxBytes > GatewayConnectionInputBytes || len(field.Choices) > 64 {
						return errors.New("native_inventory_field_identity_or_bound")
					}
					seen[field.FieldID] = true
				}
			}
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil || len(raw) > connectionInventoryMaxBytes {
		return errors.New("native_inventory_byte_bound")
	}
	return nil
}

// An old generated catalog can honestly have no export. Malformed or unknown
// exported contracts must fail instead of being disguised as an empty baseline.
func readConnectionInventoryBaseline(path string) (PlatformNativeConnectionInventory, string, error) {
	empty := PlatformNativeConnectionInventory{Contract: "native-connection-inventory/v1", Profiles: []PlatformNativeConnectionInventoryProfile{}}
	file, err := os.Open(path)
	if err != nil {
		return empty, "", err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, connectionInventoryMaxBytes+1))
	if err != nil || len(raw) > connectionInventoryMaxBytes {
		return empty, "", errors.New("native_inventory_baseline_byte_bound")
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(raw, &catalog); err != nil || catalog == nil {
		return empty, "", errors.New("native_inventory_baseline_catalog")
	}
	var envelope struct {
		Platforms           []PlatformCatalogEntry `json:"platforms"`
		CompositePrecedence []string               `json:"composite_precedence"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Platforms == nil || envelope.CompositePrecedence == nil {
		return empty, "", errors.New("native_inventory_baseline_catalog_shape")
	}
	for _, platform := range envelope.Platforms {
		if !gatewayConnectionSemanticID.MatchString(platform.ID) || platform.DisplayName == "" || platform.Gateway == "" {
			return empty, "", errors.New("native_inventory_baseline_platform_shape")
		}
	}
	for _, platformID := range envelope.CompositePrecedence {
		if !gatewayConnectionSemanticID.MatchString(platformID) {
			return empty, "", errors.New("native_inventory_baseline_precedence_shape")
		}
	}
	export, ok := catalog["native_connection_inventory"]
	if !ok {
		return empty, "not_exported", nil
	}
	var snapshot PlatformNativeConnectionInventory
	decoder := json.NewDecoder(bytes.NewReader(export))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return empty, "", err
	}
	if err := validateConnectionInventory(snapshot); err != nil {
		return empty, "", err
	}
	return snapshot, "exported", nil
}

type connectionInventoryRecord struct {
	entry connectionInventoryDelta
	value any
}

func connectionInventoryRecords(snapshot PlatformNativeConnectionInventory) map[string]map[string]connectionInventoryRecord {
	groups := map[string]map[string]connectionInventoryRecord{}
	for _, name := range []string{"products", "methods", "inputs", "requirements", "semanticRevisions"} {
		groups[name] = map[string]connectionInventoryRecord{}
	}
	for _, profile := range snapshot.Profiles {
		product := profile
		product.Methods = nil
		groups["products"][profile.ProfileID] = connectionInventoryRecord{connectionInventoryDelta{ProfileID: profile.ProfileID}, product}
		for _, method := range profile.Methods {
			key := profile.ProfileID + "/" + method.MethodID
			entry := connectionInventoryDelta{ProfileID: profile.ProfileID, MethodID: method.MethodID}
			groups["methods"][key] = connectionInventoryRecord{entry, method}
			groups["semanticRevisions"][key] = connectionInventoryRecord{entry, method.Revision}
			for scope, requirements := range map[string][]string{
				"security": method.Requirements, "configuration": method.ConfigurationRequirements, "credentialPolicies": method.AllowedCredentialPolicies,
			} {
				requirement := entry
				requirement.Scope = scope
				groups["requirements"][key+"/"+scope] = connectionInventoryRecord{requirement, requirements}
			}
			adminRequirements := entry
			adminRequirements.Scope = "admin"
			groups["requirements"][key+"/admin"] = connectionInventoryRecord{adminRequirements, method.AdminActions}
			interaction := method.Interaction
			interaction.InputFields, interaction.PreflightFields = nil, nil
			grammar := entry
			grammar.Scope = "interaction"
			groups["inputs"][key+"/interaction"] = connectionInventoryRecord{grammar, interaction}
			for scope, fields := range map[string][]GatewayConnectionField{"input": method.Interaction.InputFields, "preflight": method.Interaction.PreflightFields} {
				for _, field := range fields {
					input := entry
					input.Scope, input.FieldID = scope, field.FieldID
					groups["inputs"][key+"/"+scope+"/"+field.FieldID] = connectionInventoryRecord{input, field}
				}
			}
		}
	}
	return groups
}

func compareConnectionInventoryRecords(before, after map[string]connectionInventoryRecord) ([]connectionInventoryDelta, error) {
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := make([]connectionInventoryDelta, 0)
	for _, key := range ordered {
		old, existsBefore := before[key]
		current, existsAfter := after[key]
		if existsBefore && existsAfter && reflect.DeepEqual(old.value, current.value) {
			continue
		}
		entry := current.entry
		entry.Change = "changed"
		if !existsAfter {
			entry, entry.Change = old.entry, "removed"
		}
		if !existsBefore {
			entry.Change = "added"
		}
		var err error
		if existsBefore {
			entry.Before, err = json.Marshal(old.value)
			if err != nil {
				return nil, err
			}
		}
		if existsAfter {
			entry.After, err = json.Marshal(current.value)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

func diffConnectionInventories(before, after PlatformNativeConnectionInventory, baseline string) (connectionInventoryDiff, error) {
	result := connectionInventoryDiff{Contract: "native-connection-inventory-diff/v1", Baseline: baseline}
	if err := validateConnectionInventory(before); err != nil {
		return result, err
	}
	if err := validateConnectionInventory(after); err != nil {
		return result, err
	}
	old, current := connectionInventoryRecords(before), connectionInventoryRecords(after)
	for name, target := range map[string]*[]connectionInventoryDelta{
		"products": &result.Products, "methods": &result.Methods, "inputs": &result.Inputs,
		"requirements": &result.Requirements, "semanticRevisions": &result.SemanticRevisions,
	} {
		changes, err := compareConnectionInventoryRecords(old[name], current[name])
		if err != nil {
			return result, err
		}
		*target = changes
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 4*connectionInventoryMaxBytes {
		return result, errors.New("native_inventory_diff_byte_bound")
	}
	return result, nil
}

func TestGatewayConnectionInventoryQualificationExport(t *testing.T) {
	current := BuildPlatformCatalog().NativeConnectionInventory
	require.NoError(t, validateConnectionInventory(current))
	baselinePath, exportPath, reportPath := os.Getenv("NATIVE_INVENTORY_BASELINE"), os.Getenv("NATIVE_INVENTORY_EXPORT"), os.Getenv("NATIVE_INVENTORY_REPORT")
	if baselinePath == "" && exportPath == "" && reportPath == "" {
		return
	}
	require.NotEmpty(t, baselinePath)
	require.NotEmpty(t, exportPath)
	require.NotEmpty(t, reportPath)
	before, baseline, err := readConnectionInventoryBaseline(baselinePath)
	require.NoError(t, err)
	report, err := diffConnectionInventories(before, current, baseline)
	require.NoError(t, err)
	report.BaseRevision, report.CurrentRevision = os.Getenv("NATIVE_INVENTORY_BASE_REVISION"), os.Getenv("NATIVE_INVENTORY_CURRENT_REVISION")
	for _, output := range []struct {
		path     string
		value    any
		maxBytes int
	}{{exportPath, current, connectionInventoryMaxBytes}, {reportPath, report, 4 * connectionInventoryMaxBytes}} {
		raw, err := json.MarshalIndent(output.value, "", "  ")
		require.NoError(t, err)
		require.LessOrEqual(t, len(raw)+1, output.maxBytes, "formatted inventory artifact exceeds its byte bound")
		require.NoError(t, os.WriteFile(output.path, append(raw, '\n'), 0o644))
	}
	t.Logf("inventory baseline=%s profiles=%d products=%d methods=%d inputs=%d requirements=%d revisions=%d", baseline,
		len(current.Profiles), len(report.Products), len(report.Methods), len(report.Inputs), len(report.Requirements), len(report.SemanticRevisions))
}

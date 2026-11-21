package service

import (
	"context"
	"net/url"
	"strings"
)

// These are adapters to the deployed OAuth services, not another lifecycle.
// Concrete services retain their durable claims, custody, refresh and dispatch.
type gatewayBoundOAuthPorts struct {
	begin    func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeOAuthConnectResult, error)
	read     func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthConnectResult, error)
	submit   func(context.Context, GatewayNativeConnectionScope, map[string]string) (GatewayNativeOAuthConnectResult, error)
	poll     func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthConnectResult, error)
	describe func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthDescriptorResult, error)
	refresh  func(context.Context, GatewayNativeCredentialScope, string) (GatewayNativeOAuthMaintenanceOutcome, error)
	custody  func(context.Context, GatewayNativeCredentialScope, string, GatewayNativeRoute) (GatewayNativeRetainedCustodyResult, error)
	cleanup  func(context.Context, GatewayNativeOAuthCleanupRequest) (GatewayNativeOAuthCleanupResult, error)
}

func gatewayConnectionDefinition(in GatewayNativeConnectionScope) (GatewayConnectionMethodDefinition, error) {
	profile, ok := LookupGatewayConnectionProfile(in.Connection.ProfileID)
	if ok {
		for _, method := range profile.Methods {
			if method.MethodID == in.Connection.MethodID && method.Revision == in.Connection.MethodRevision {
				return method, nil
			}
		}
	}
	return GatewayConnectionMethodDefinition{}, ErrGatewayNativeIdentity
}

func gatewayConnectionInputFor(in GatewayNativeConnectionScope, input map[string]string, completion bool) error {
	method, err := gatewayConnectionDefinition(in)
	if err != nil {
		return err
	}
	return gatewayConnectionDefinitionInput(method, input, completion)
}

func gatewayConnectionDefinitionInput(method GatewayConnectionMethodDefinition, input map[string]string, completion bool) error {
	fields := method.Interaction.InputFields
	if method.Interaction.Kind != "credential_form" {
		fields = method.Interaction.PreflightFields
		if completion {
			if method.Interaction.Kind != "browser" {
				return ErrGatewayNativeIdentity
			}
			fields = []GatewayConnectionField{gatewayConnectionField("completion", "Authorization response", "secret", true, 8192)}
		}
	}
	return ValidateGatewayConnectionInput(fields, input)
}

func newGatewayBoundOAuthAdapter(profile, dispatchFamily, authFamily string, methods []string, ports gatewayBoundOAuthPorts) (GatewayNativeConnectionAdapter, error) {
	var empty GatewayNativeConnectionAdapter
	if ports.begin == nil || ports.read == nil || ports.submit == nil || ports.describe == nil || ports.refresh == nil || ports.custody == nil || ports.cleanup == nil {
		return empty, ErrGatewayNativeIdentity
	}
	credential := GatewayCredentialAttestation{CredentialKind: "oauth", CredentialPolicy: "subscription_session", RefreshPolicy: "native"}
	publication := func(in GatewayNativeConnectionScope) GatewayConnectionPublication {
		return GatewayConnectionPublication{Connection: in.Connection, Credential: credential, DispatchFamily: dispatchFamily, AuthFamily: authFamily}
	}
	authorize := func(ctx context.Context, in GatewayNativeConnectionScope) (context.Context, error) {
		found := false
		for _, method := range methods {
			found = found || in.Connection.MethodID == method
		}
		if !found {
			return nil, ErrGatewayNativeIdentity
		}
		return in.Authorize(ctx, GatewayOAuthBundlePurpose, profile)
	}
	view := func(out GatewayNativeOAuthConnectResult, in GatewayNativeConnectionScope, err error) (GatewayNativeConnectionResult, error) {
		if err != nil {
			return GatewayNativeConnectionResult{}, err
		}
		if !SameGatewayConnectionSelection(out.Connection, &in.Connection) || out.Operation != in.Operation || out.Account != in.Scope.Account {
			return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
		}
		result := GatewayNativeConnectionResult{GatewayNativeOAuthConnectResult: out}
		if out.State == "completed" && out.Outcome == "staged" {
			actual := credential
			result.Credential = &actual
		}
		return result, nil
	}
	read := func(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionResult, error) {
		ctx, err := authorize(ctx, in)
		if err != nil {
			return GatewayNativeConnectionResult{}, err
		}
		out, err := ports.read(ctx, in.Scope, in.Operation)
		return view(out, in, err)
	}
	adapter := GatewayNativeConnectionAdapter{ProfileID: profile, Purpose: GatewayOAuthBundlePurpose, MethodIDs: append([]string(nil), methods...), DispatchFamily: dispatchFamily, AuthFamily: authFamily, Read: read}
	adapter.Begin = func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeConnectionResult, error) {
		ctx, err := authorize(ctx, in)
		if err != nil || gatewayConnectionInputFor(in, input, false) != nil {
			return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
		}
		out, err := ports.begin(ctx, in, input)
		return view(out, in, err)
	}
	adapter.Submit = func(ctx context.Context, in GatewayNativeConnectionScope, input map[string]string) (GatewayNativeConnectionResult, error) {
		ctx, err := authorize(ctx, in)
		if err != nil || gatewayConnectionInputFor(in, input, true) != nil {
			return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
		}
		out, err := ports.submit(ctx, in, input)
		return view(out, in, err)
	}
	if ports.poll != nil {
		adapter.Poll = func(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionResult, error) {
			ctx, err := authorize(ctx, in)
			if err != nil || in.Connection.MethodID != "device_code" {
				return GatewayNativeConnectionResult{}, ErrGatewayNativeIdentity
			}
			out, err := ports.poll(ctx, in.Scope, in.Operation)
			return view(out, in, err)
		}
	}
	adapter.Describe = func(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionDescriptorResult, error) {
		var result GatewayNativeConnectionDescriptorResult
		ctx, err := authorize(ctx, in)
		if err != nil {
			return result, err
		}
		if _, err = read(ctx, in); err != nil {
			return result, err
		}
		out, err := ports.describe(ctx, in.Scope, in.Operation)
		if err != nil || out.Operation != in.Operation || out.Account != in.Scope.Account {
			return result, ErrGatewayNativeIdentity
		}
		result = GatewayNativeConnectionDescriptorResult{Connection: in.Connection, Operation: out.Operation, Account: out.Account, State: out.State, Qualification: out.Qualification}
		if out.Native != nil {
			if out.Native.Profile != profile || out.Native.Generation != in.Scope.Generation {
				return GatewayNativeConnectionDescriptorResult{}, ErrGatewayNativeIdentity
			}
			result.Native = &GatewayNativePublishedRoute{GatewayNativeRoute: *out.Native, Publication: publication(in)}
		}
		return result, nil
	}
	adapter.Refresh = func(ctx context.Context, in GatewayNativeConnectionScope) (GatewayNativeConnectionMaintenanceResult, error) {
		ctx, err := authorize(ctx, in)
		if err != nil {
			return GatewayNativeConnectionMaintenanceResult{}, err
		}
		if _, err = read(ctx, in); err != nil {
			return GatewayNativeConnectionMaintenanceResult{}, err
		}
		out, err := ports.refresh(ctx, in.Scope, in.Operation)
		if err != nil {
			return GatewayNativeConnectionMaintenanceResult{}, err
		}
		return GatewayNativeConnectionMaintenanceResult{GatewayNativeOAuthMaintenanceOutcome: out, Connection: in.Connection}, nil
	}
	adapter.Custody = func(ctx context.Context, in GatewayNativeConnectionScope, route GatewayNativePublishedRoute) (GatewayNativeConnectionCustodyResult, error) {
		var result GatewayNativeConnectionCustodyResult
		ctx, err := authorize(ctx, in)
		if err != nil || route.Publication != publication(in) || route.Profile != profile || route.Generation != in.Scope.Generation {
			return result, ErrGatewayNativeIdentity
		}
		if _, err = read(ctx, in); err != nil {
			return result, err
		}
		out, err := ports.custody(ctx, in.Scope, in.Operation, route.GatewayNativeRoute)
		if err != nil || !out.Proof.Authenticated || len(out.CredentialKinds) != 1 || out.CredentialKinds[0] != credential.CredentialKind || !SameGatewayNativeDescriptor(out.Native, route.GatewayNativeRoute) {
			return result, ErrGatewayNativeIdentity
		}
		return GatewayNativeConnectionCustodyResult{Contract: "native-retained-custody/v2", Generation: out.Generation, Native: route, CredentialKinds: out.CredentialKinds, Retention: out.Retention, Proof: out.Proof}, nil
	}
	adapter.Cleanup = func(ctx context.Context, in GatewayNativeConnectionScope, request GatewayNativeOAuthCleanupRequest) (GatewayNativeConnectionCleanupResult, error) {
		var result GatewayNativeConnectionCleanupResult
		ctx, err := authorize(ctx, in)
		if err != nil || request.Scope != in.Scope || request.Operation != in.Operation {
			return result, ErrGatewayNativeIdentity
		}
		if _, err = read(ctx, in); err != nil {
			return result, err
		}
		out, err := ports.cleanup(ctx, request)
		if err != nil {
			return result, err
		}
		result = GatewayNativeConnectionCleanupResult{Connection: in.Connection, Operation: out.Operation, Account: out.Account, CredentialsErased: out.CredentialsErased, Closure: out.Closure}
		if out.Native != nil {
			result.Native = &GatewayNativePublishedRoute{GatewayNativeRoute: *out.Native, Publication: publication(in)}
		}
		return result, nil
	}
	return adapter, nil
}

// Callback grammar belongs to native. Both origin/path and duplicate parameters
// are checked before handing state/code to the existing one-entry completion.
func gatewayConnectionCallback(input, expectedRedirect string) (string, string, error) {
	if len(input) == 0 || len(input) > 8192 || strings.TrimSpace(input) != input {
		return "", "", ErrGatewayNativeIdentity
	}
	u, err := url.Parse(input)
	expected, expectedErr := url.Parse(expectedRedirect)
	if err != nil || expectedErr != nil || u.User != nil || u.Fragment != "" || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.RawPath != "" || u.Opaque != "" {
		return "", "", ErrGatewayNativeIdentity
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 2 || len(query["state"]) != 1 || len(query["code"]) != 1 || !GatewayNativeOAuthConnectCallbackValid(query.Get("state"), query.Get("code")) {
		return "", "", ErrGatewayNativeIdentity
	}
	return query.Get("state"), query.Get("code"), nil
}

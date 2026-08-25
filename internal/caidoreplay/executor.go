// Package caidoreplay is the deliberately narrow active boundary used by the
// MCP Replay tools. It can only create/update HTTP Replay entries and start a
// Replay task; observation tools continue to depend exclusively on
// caidoread.Reader.
package caidoreplay

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	gql "github.com/Khan/genqlient/graphql"
	caido "github.com/caido-community/sdk-go"
	gen "github.com/caido-community/sdk-go/graphql"
	"github.com/ph-xyz/Caido-Bridge/internal/config"
)

const (
	defaultReplayTimeout = 12 * time.Second
	maxEncodedMessage    = 8 * 1024 * 1024
)

// Connection is the immutable target connection for one controlled Replay.
type Connection struct {
	Host string
	Port int
	TLS  bool
}

// Result is factual evidence captured from one Caido Replay entry.
type Result struct {
	SessionID   string
	EntryID     string
	RequestRaw  []byte
	ResponseRaw []byte
	StatusCode  int
	RoundtripMS int
	Length      int
}

// Executor is the complete active surface available to the MCP tool layer.
// Start creates one Replay session. Continue sends one additional draft in the
// same session so baseline and test stay associated in Caido.
type Executor interface {
	Start(context.Context, Connection, []byte) (Result, error)
	Continue(context.Context, string, string, Connection, []byte) (Result, error)
}

// SDKExecutor adapts the Caido 0.57+ draft-then-start Replay contract.
type SDKExecutor struct {
	client  *caido.Client
	timeout time.Duration
}

// New creates an active Replay client that can only contact the exact local
// Caido GraphQL origin. The access token is injected into a cloned local
// request and redirects are never followed.
func New(caidoURL, accessToken string) (*SDKExecutor, error) {
	validatedURL, err := config.ValidateLocalURL(caidoURL)
	if err != nil {
		return nil, fmt.Errorf("validate local Caido URL: %w", err)
	}
	origin, err := url.Parse(validatedURL)
	if err != nil {
		return nil, fmt.Errorf("parse local Caido URL: %w", err)
	}
	httpClient := &http.Client{
		Transport: &localGraphQLTransport{
			base:   http.DefaultTransport,
			origin: origin,
			token:  accessToken,
		},
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	client, err := caido.NewClient(caido.Options{URL: validatedURL})
	if err != nil {
		return nil, fmt.Errorf("create Caido SDK client: %w", err)
	}
	// ReplaySDK resolves GraphQL through the exported Client.GraphQL field.
	// Replacing it keeps the SDK's domain adapter while retaining our stricter
	// same-origin transport and avoiding the SDK's redirect-following client.
	client.GraphQL = gql.NewClient(validatedURL+"/graphql", httpClient)
	return &SDKExecutor{client: client, timeout: defaultReplayTimeout}, nil
}

func (e *SDKExecutor) Start(
	ctx context.Context,
	connection Connection,
	raw []byte,
) (Result, error) {
	encoded := base64.StdEncoding.EncodeToString(raw)
	sessionID, seedEntryID, err := e.client.Replay.CreateSessionWithRaw(
		ctx,
		caido.ReplayConnection{
			Host:  connection.Host,
			Port:  connection.Port,
			IsTLS: connection.TLS,
		},
		encoded,
	)
	if err != nil {
		return Result{}, fmt.Errorf("create Replay session: %w", err)
	}
	if sessionID == "" || seedEntryID == "" {
		return Result{}, fmt.Errorf("create Replay session returned no active entry")
	}
	return e.startAndWait(ctx, sessionID, seedEntryID)
}

func (e *SDKExecutor) Continue(
	ctx context.Context,
	sessionID string,
	activeEntryID string,
	connection Connection,
	raw []byte,
) (Result, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(activeEntryID) == "" {
		return Result{}, fmt.Errorf("Replay session and active entry IDs are required")
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	err := e.client.Replay.UpdateEntryDraft(
		ctx,
		activeEntryID,
		caido.ReplayConnection{
			Host:  connection.Host,
			Port:  connection.Port,
			IsTLS: connection.TLS,
		},
		encoded,
		nil,
	)
	if err != nil {
		return Result{}, fmt.Errorf("update Replay draft: %w", err)
	}
	return e.startAndWait(ctx, sessionID, activeEntryID)
}

func (e *SDKExecutor) startAndWait(
	ctx context.Context,
	sessionID string,
	previousEntryID string,
) (Result, error) {
	started, err := e.client.Replay.StartTask(ctx, sessionID)
	if err != nil {
		return Result{}, fmt.Errorf("start Replay task: %w", err)
	}
	if err := validateStartedTask(started); err != nil {
		return Result{}, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return Result{}, ctx.Err()
			}
			return Result{}, fmt.Errorf("Replay response timeout after %s", e.timeout)
		case <-ticker.C:
			session, err := e.client.Replay.GetSession(waitCtx, sessionID)
			if err != nil {
				return Result{}, fmt.Errorf("poll Replay session: %w", err)
			}
			if session == nil || session.ActiveEntryID == "" ||
				session.ActiveEntryID == previousEntryID {
				continue
			}
			entry, err := e.client.Replay.GetEntry(
				waitCtx,
				session.ActiveEntryID,
				gen.ReplaySessionKindHttp,
			)
			if err != nil {
				return Result{}, fmt.Errorf("read Replay entry: %w", err)
			}
			if entry == nil {
				continue
			}
			if entry.Error != nil && strings.TrimSpace(*entry.Error) != "" {
				return Result{}, fmt.Errorf("Caido Replay error: %s", *entry.Error)
			}
			if entry.Request == nil || entry.Request.Response == nil {
				continue
			}

			requestRaw, err := decodeMessage("Replay request", entry.Request.Raw)
			if err != nil {
				return Result{}, err
			}
			responseRaw, err := decodeMessage(
				"Replay response",
				entry.Request.Response.Raw,
			)
			if err != nil {
				return Result{}, err
			}
			return Result{
				SessionID:   sessionID,
				EntryID:     session.ActiveEntryID,
				RequestRaw:  requestRaw,
				ResponseRaw: responseRaw,
				StatusCode:  entry.Request.Response.StatusCode,
				RoundtripMS: entry.Request.Response.RoundtripTime,
				Length:      entry.Request.Response.Length,
			}, nil
		}
	}
}

func validateStartedTask(response *gen.StartReplayTaskResponse) error {
	if response == nil {
		return fmt.Errorf("start Replay task returned no result")
	}
	if caido.IsTaskInProgress(response) {
		return fmt.Errorf("Replay session is already running another task")
	}
	if errorValue := response.StartReplayTask.GetError(); errorValue != nil {
		errorType := "unknown"
		if typeName := (*errorValue).GetTypename(); typeName != nil && *typeName != "" {
			errorType = *typeName
		}
		if other, ok := (*errorValue).(*gen.StartReplayTaskStartReplayTaskStartReplayTaskPayloadErrorOtherUserError); ok && other.Code != "" {
			return fmt.Errorf("Caido rejected Replay task (%s, code %s)", errorType, other.Code)
		}
		return fmt.Errorf("Caido rejected Replay task (%s)", errorType)
	}
	if response.StartReplayTask.GetTask() == nil {
		return fmt.Errorf("start Replay task returned neither task nor error")
	}
	return nil
}

func decodeMessage(label, encoded string) ([]byte, error) {
	if len(encoded) > maxEncodedMessage {
		return nil, fmt.Errorf("%s exceeds the %d-byte encoded limit", label, maxEncodedMessage)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	return raw, nil
}

type localGraphQLTransport struct {
	base   http.RoundTripper
	origin *url.URL
	token  string
}

func (t *localGraphQLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !sameOrigin(req.URL, t.origin) {
		return nil, fmt.Errorf("refusing Replay API request outside configured local Caido origin")
	}
	if req.Method != http.MethodPost || req.URL.Path != "/graphql" {
		return nil, fmt.Errorf("refusing unexpected local Caido Replay API endpoint or method")
	}
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(cloned)
}

func sameOrigin(candidate, origin *url.URL) bool {
	return strings.EqualFold(candidate.Scheme, origin.Scheme) &&
		strings.EqualFold(candidate.Host, origin.Host)
}

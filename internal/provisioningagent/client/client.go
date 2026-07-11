// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	retry "github.com/avast/retry-go/v4"

	nodelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	requestTimeout = 30 * time.Second
	maxAttempts    = 3
)

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	TrustStore agentprotocol.TrustStore
	RetryDelay func(attempt uint) time.Duration
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	trustStore agentprotocol.TrustStore
	retryDelay func(attempt uint) time.Duration
}

type APIError struct {
	StatusCode int
	Code       string
}

func (err *APIError) Error() string {
	return fmt.Sprintf("agent API returned status %d (%s)", err.StatusCode, err.Code)
}

func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Agent API URL: %w", err)
	}
	if baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, errors.New("agent API URL must use HTTPS and include a host")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("agent API URL must not contain credentials, query, or fragment")
	}
	if strings.Trim(baseURL.Path, "/") != "" {
		return nil, errors.New("agent API URL must not contain a path")
	}
	if config.TrustStore == nil {
		return nil, errors.New("plan trust store is required")
	}

	httpClient := http.DefaultClient
	if config.HTTPClient != nil {
		httpClient = config.HTTPClient
	}
	clientCopy := *httpClient
	clientCopy.Timeout = requestTimeout
	// redirect先へAuthorization headerを転送しないため、全redirectを明示的に拒否する。
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	retryDelay := config.RetryDelay
	if retryDelay == nil {
		retryDelay = defaultRetryDelay
	}
	baseURL.Path = ""
	return &Client{
		baseURL:    baseURL,
		httpClient: &clientCopy,
		trustStore: config.TrustStore,
		retryDelay: retryDelay,
	}, nil
}

func (client *Client) Register(
	ctx context.Context,
	request agentprotocol.RegisterRequest,
) (agentprotocol.RegisterResponse, error) {
	var response agentprotocol.RegisterResponse
	if err := client.doJSON(ctx, http.MethodPost, "/v1/agent/register", "", request, &response, agentprotocol.MaxRequestBodyBytes); err != nil {
		return agentprotocol.RegisterResponse{}, fmt.Errorf("register Agent: %w", err)
	}
	if response.APIVersion != agentprotocol.APIVersion ||
		response.SessionToken == "" ||
		response.PlanDigest == "" ||
		response.ExpiresAt.IsZero() ||
		response.AgentSequence < 0 {
		return agentprotocol.RegisterResponse{}, errors.New("register Agent: response is invalid")
	}
	return response, nil
}

func (client *Client) FetchPlan(
	ctx context.Context,
	operationUID, sessionToken, expectedDigest string,
) (agentprotocol.ValidatedPlan, error) {
	var signed agentprotocol.SignedPlan
	endpoint, err := operationEndpoint(operationUID, "plan")
	if err != nil {
		return agentprotocol.ValidatedPlan{}, err
	}
	if err := client.doJSON(ctx, http.MethodGet, endpoint, sessionToken, nil, &signed, agentprotocol.MaxRequestBodyBytes); err != nil {
		return agentprotocol.ValidatedPlan{}, fmt.Errorf("fetch Plan: %w", err)
	}
	validated, err := agentprotocol.ValidatePlan(signed.Plan)
	if err != nil {
		return agentprotocol.ValidatedPlan{}, fmt.Errorf("validate Plan: %w", err)
	}
	if signed.Plan.OperationUID != operationUID {
		return agentprotocol.ValidatedPlan{}, errors.New("validate Plan: operation UID does not match request")
	}
	actualDigest, err := validated.Digest()
	if err != nil {
		return agentprotocol.ValidatedPlan{}, fmt.Errorf("digest Plan: %w", err)
	}
	if actualDigest.String() != expectedDigest {
		return agentprotocol.ValidatedPlan{}, errors.New("validate Plan: digest does not match registration")
	}
	if err := agentprotocol.VerifySignature(validated, signed.Signature, client.trustStore); err != nil {
		return agentprotocol.ValidatedPlan{}, fmt.Errorf("verify Plan signature: %w", err)
	}
	return validated, nil
}

func (client *Client) FetchNodeLifecyclePlan(
	ctx context.Context,
	operationUID, sessionToken, expectedDigest string,
) (nodelifecycle.ValidatedPlan, error) {
	var signed nodelifecycle.SignedPlan
	endpoint, err := operationEndpoint(operationUID, "node-lifecycle-plan")
	if err != nil {
		return nodelifecycle.ValidatedPlan{}, err
	}
	if err := client.doJSON(ctx, http.MethodGet, endpoint, sessionToken, nil, &signed, agentprotocol.MaxRequestBodyBytes); err != nil {
		return nodelifecycle.ValidatedPlan{}, fmt.Errorf("fetch Node Lifecycle Plan: %w", err)
	}
	validated, err := nodelifecycle.ValidatePlan(signed.Plan)
	if err != nil {
		return nodelifecycle.ValidatedPlan{}, fmt.Errorf("validate Node Lifecycle Plan: %w", err)
	}
	if signed.Plan.OperationID != operationUID {
		return nodelifecycle.ValidatedPlan{}, errors.New("validate Node Lifecycle Plan: operation UID does not match request")
	}
	actualDigest, err := validated.Digest()
	if err != nil {
		return nodelifecycle.ValidatedPlan{}, fmt.Errorf("digest Node Lifecycle Plan: %w", err)
	}
	if actualDigest.String() != expectedDigest {
		return nodelifecycle.ValidatedPlan{}, errors.New("validate Node Lifecycle Plan: digest does not match registration")
	}
	if err := nodelifecycle.VerifySignature(validated, signed.Signature, client.trustStore); err != nil {
		return nodelifecycle.ValidatedPlan{}, fmt.Errorf("verify Node Lifecycle Plan signature: %w", err)
	}
	return validated, nil
}

func (client *Client) ReportProgress(
	ctx context.Context,
	sessionToken string,
	request agentprotocol.ProgressRequest,
) (agentprotocol.ProgressResponse, error) {
	var response agentprotocol.ProgressResponse
	endpoint, err := operationEndpoint(request.OperationUID, "progress")
	if err != nil {
		return agentprotocol.ProgressResponse{}, err
	}
	if err := client.doJSON(ctx, http.MethodPost, endpoint, sessionToken, request, &response, agentprotocol.MaxRequestBodyBytes); err != nil {
		return agentprotocol.ProgressResponse{}, fmt.Errorf("report Agent progress: %w", err)
	}
	if response.APIVersion != agentprotocol.APIVersion {
		return agentprotocol.ProgressResponse{}, errors.New("report Agent progress: response apiVersion is invalid")
	}
	return response, nil
}

func (client *Client) FetchBootstrap(
	ctx context.Context,
	operationUID, sessionToken string,
) (agentprotocol.BootstrapBundle, error) {
	var bundle agentprotocol.BootstrapBundle
	endpoint, err := operationEndpoint(operationUID, "bootstrap")
	if err != nil {
		return agentprotocol.BootstrapBundle{}, err
	}
	if err := client.doJSON(ctx, http.MethodGet, endpoint, sessionToken, nil, &bundle, agentprotocol.MaxBootstrapBodyBytes); err != nil {
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("fetch Bootstrap Bundle: %w", err)
	}
	if err := agentprotocol.ValidateBootstrapBundle(bundle); err != nil {
		return agentprotocol.BootstrapBundle{}, fmt.Errorf("validate Bootstrap Bundle: %w", err)
	}
	if bundle.OperationUID != operationUID {
		return agentprotocol.BootstrapBundle{}, errors.New("validate Bootstrap Bundle: operation UID does not match request")
	}
	return bundle, nil
}

func (client *Client) ReportBoot(
	ctx context.Context,
	sessionToken string,
	request agentprotocol.BootReportRequest,
) error {
	if err := agentprotocol.ValidateBootReport(request); err != nil {
		return fmt.Errorf("validate boot report: %w", err)
	}
	endpoint, err := operationEndpoint(request.OperationUID, "boot-report")
	if err != nil {
		return err
	}
	if err := client.doJSON(ctx, http.MethodPost, endpoint, sessionToken, request, nil, 0); err != nil {
		return fmt.Errorf("report Agent boot: %w", err)
	}
	return nil
}

func (client *Client) doJSON(
	ctx context.Context,
	method, endpoint, sessionToken string,
	requestBody, responseBody any,
	maxResponseBytes int64,
) error {
	encoded, err := encodeRequest(requestBody)
	if err != nil {
		return err
	}
	return retry.Do(
		func() error {
			attemptContext, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			request, err := http.NewRequestWithContext(
				attemptContext,
				method,
				client.baseURL.ResolveReference(&url.URL{Path: endpoint}).String(),
				bytes.NewReader(encoded),
			)
			if err != nil {
				return retry.Unrecoverable(fmt.Errorf("create Agent API request: %w", err))
			}
			request.Header.Set("Accept", "application/json")
			if requestBody != nil {
				request.Header.Set("Content-Type", "application/json")
			}
			if sessionToken != "" {
				request.Header.Set("Authorization", "Bearer "+sessionToken)
			}
			response, err := client.httpClient.Do(request)
			if err != nil {
				return fmt.Errorf("call Agent API: %w", err)
			}
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				apiErr := decodeAPIError(response)
				if err := response.Body.Close(); err != nil {
					return fmt.Errorf("close Agent API error response: %w", err)
				}
				if retryableStatus(response.StatusCode) {
					return apiErr
				}
				return retry.Unrecoverable(apiErr)
			}
			if responseBody == nil {
				if err := response.Body.Close(); err != nil {
					return fmt.Errorf("close Agent API response: %w", err)
				}
				return nil
			}
			if err := decodeResponse(response.Body, responseBody, maxResponseBytes); err != nil {
				closeErr := response.Body.Close()
				return retry.Unrecoverable(errors.Join(fmt.Errorf("decode Agent API response: %w", err), closeErr))
			}
			if err := response.Body.Close(); err != nil {
				return fmt.Errorf("close Agent API response: %w", err)
			}
			return nil
		},
		retry.Attempts(maxAttempts),
		retry.Context(ctx),
		retry.DelayType(func(attempt uint, _ error, _ *retry.Config) time.Duration {
			return client.retryDelay(attempt)
		}),
		retry.LastErrorOnly(true),
	)
}

func decodeResponse(reader io.Reader, target any, maxBytes int64) error {
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return errors.New("response body exceeds the endpoint limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	err = decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("response must contain exactly one JSON value")
}

func operationEndpoint(operationUID, action string) (string, error) {
	if operationUID == "" || strings.Contains(operationUID, "/") ||
		operationUID == "." || operationUID == ".." {
		return "", errors.New("operation UID cannot be represented as an API path segment")
	}
	return "/v1/operations/" + operationUID + "/" + action, nil
}

func encodeRequest(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Agent API request: %w", err)
	}
	if len(encoded) > agentprotocol.MaxRequestBodyBytes {
		return nil, agentprotocol.ErrRequestTooLarge
	}
	return encoded, nil
}

func decodeAPIError(response *http.Response) *APIError {
	var body agentprotocol.ErrorResponse
	limited := io.LimitReader(response.Body, agentprotocol.MaxRequestBodyBytes)
	_ = json.NewDecoder(limited).Decode(&body)
	return &APIError{StatusCode: response.StatusCode, Code: body.Code}
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusTooEarly ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

func defaultRetryDelay(attempt uint) time.Duration {
	base := time.Second
	if attempt > 1 {
		base = 2 * time.Second
	}
	factor := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(base) * factor)
}

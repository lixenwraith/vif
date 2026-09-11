package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const maxKubeResponse = 4 << 20

type kubeAPI interface {
	listJobs(context.Context) ([]job, error)
	listServices(context.Context) ([]service, error)
	listPods(context.Context, string) ([]pod, error)
	listEndpointSlices(context.Context, string) ([]endpointSlice, error)
	createJob(context.Context, map[string]any) (job, error)
	createService(context.Context, map[string]any) (service, error)
	deleteJob(context.Context, string) error
	deleteService(context.Context, string) error
}

type kubeClient struct {
	baseURL   *url.URL
	namespace string
	tokenFile string
	http      *http.Client
}

type apiError struct {
	StatusCode int
	Reason     string
	Message    string
}

func (e *apiError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("kubernetes API: %s: %s", e.Reason, e.Message)
	}
	return fmt.Sprintf("kubernetes API: HTTP %d: %s", e.StatusCode, e.Message)
}

type statusResponse struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func newKubeClient(apiURL, namespace, caFile, tokenFile string, requestTimeout time.Duration) (*kubeClient, error) {
	baseURL, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse Kubernetes API URL: %w", err)
	}
	if baseURL.Scheme != "https" || baseURL.Host == "" {
		return nil, fmt.Errorf("Kubernetes API URL must be an absolute https URL")
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes CA file: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil || rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("Kubernetes CA file contains no certificates")
	}

	transport := &http.Transport{
		// The API is node-local. Never let a process-wide proxy setting send the
		// allocator credential or Kubernetes traffic off the guest.
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: requestTimeout,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    rootCAs,
		},
	}

	return &kubeClient{
		baseURL:   baseURL,
		namespace: namespace,
		tokenFile: tokenFile,
		http:      &http.Client{Transport: transport, Timeout: requestTimeout},
	}, nil
}

func (c *kubeClient) listJobs(ctx context.Context) ([]job, error) {
	var list kubeList[job]
	err := c.doJSON(ctx, http.MethodGet, c.batchPath("jobs"), nil, nil, &list)
	return list.Items, err
}

func (c *kubeClient) listServices(ctx context.Context) ([]service, error) {
	var list kubeList[service]
	err := c.doJSON(ctx, http.MethodGet, c.corePath("services"), nil, nil, &list)
	return list.Items, err
}

func (c *kubeClient) listPods(ctx context.Context, labelSelector string) ([]pod, error) {
	query := url.Values{}
	if labelSelector != "" {
		query.Set("labelSelector", labelSelector)
	}
	var list kubeList[pod]
	err := c.doJSON(ctx, http.MethodGet, c.corePath("pods"), query, nil, &list)
	return list.Items, err
}

func (c *kubeClient) listEndpointSlices(ctx context.Context, labelSelector string) ([]endpointSlice, error) {
	query := url.Values{}
	if labelSelector != "" {
		query.Set("labelSelector", labelSelector)
	}
	var list kubeList[endpointSlice]
	err := c.doJSON(ctx, http.MethodGet, c.discoveryPath("endpointslices"), query, nil, &list)
	return list.Items, err
}

func (c *kubeClient) createJob(ctx context.Context, object map[string]any) (job, error) {
	var created job
	err := c.doJSON(ctx, http.MethodPost, c.batchPath("jobs"), nil, object, &created)
	return created, err
}

func (c *kubeClient) createService(ctx context.Context, object map[string]any) (service, error) {
	var created service
	err := c.doJSON(ctx, http.MethodPost, c.corePath("services"), nil, object, &created)
	return created, err
}

func (c *kubeClient) deleteJob(ctx context.Context, name string) error {
	options := map[string]any{
		"apiVersion":        "v1",
		"kind":              "DeleteOptions",
		"propagationPolicy": "Background",
	}
	err := c.doJSON(ctx, http.MethodDelete, c.batchPath(path.Join("jobs", name)), nil, options, nil)
	if isAPIStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (c *kubeClient) deleteService(ctx context.Context, name string) error {
	options := map[string]any{
		"apiVersion": "v1",
		"kind":       "DeleteOptions",
	}
	err := c.doJSON(ctx, http.MethodDelete, c.corePath(path.Join("services", name)), nil, options, nil)
	if isAPIStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (c *kubeClient) batchPath(resource string) string {
	return path.Join("/apis/batch/v1/namespaces", c.namespace, resource)
}

func (c *kubeClient) corePath(resource string) string {
	return path.Join("/api/v1/namespaces", c.namespace, resource)
}

func (c *kubeClient) discoveryPath(resource string) string {
	return path.Join("/apis/discovery.k8s.io/v1/namespaces", c.namespace, resource)
}

func (c *kubeClient) doJSON(
	ctx context.Context,
	method string,
	requestPath string,
	query url.Values,
	body any,
	result any,
) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Kubernetes request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	token, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return fmt.Errorf("read Kubernetes token: %w", err)
	}
	bearer := strings.TrimSpace(string(token))
	if bearer == "" {
		return fmt.Errorf("Kubernetes token file is empty")
	}

	requestURL := *c.baseURL
	requestURL.Path = path.Join(c.baseURL.Path, requestPath)
	requestURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), requestBody)
	if err != nil {
		return fmt.Errorf("build Kubernetes request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Kubernetes request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxKubeResponse+1))
	if err != nil {
		return fmt.Errorf("read Kubernetes response: %w", err)
	}
	if len(data) > maxKubeResponse {
		return fmt.Errorf("Kubernetes response exceeds %d bytes", maxKubeResponse)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var status statusResponse
		_ = json.Unmarshal(data, &status)
		if status.Message == "" {
			status.Message = strings.TrimSpace(string(data))
		}
		return &apiError{StatusCode: resp.StatusCode, Reason: status.Reason, Message: status.Message}
	}
	if result == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode Kubernetes response: %w", err)
	}
	return nil
}

func isAPIStatus(err error, status int) bool {
	var target *apiError
	return errors.As(err, &target) && target.StatusCode == status
}

func isNodePortConflict(err error) bool {
	var target *apiError
	if !errors.As(err, &target) || (target.StatusCode != http.StatusConflict && target.StatusCode != http.StatusUnprocessableEntity) {
		return false
	}
	message := strings.ToLower(target.Message)
	return strings.Contains(message, "nodeport") || strings.Contains(message, "provided port is already allocated")
}

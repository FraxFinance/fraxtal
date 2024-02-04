package fraxda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/multiformats/go-multibase"
)

type DAClient struct {
	baseUrl    *url.URL
	httpClient *http.Client
}

func NewDAClient(rpc string) (*DAClient, error) {
	baseUrl, err := url.Parse(rpc)
	if err != nil {
		return nil, fmt.Errorf("unable to parse DA endpoint: %s", err)
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &DAClient{
		baseUrl:    baseUrl,
		httpClient: httpClient,
	}, nil
}

func (c DAClient) Read(ctx context.Context, id []byte) ([]byte, error) {
	ipfsCID, err := multibase.Encode(multibase.Base32, id)
	if err != nil {
		return nil, fmt.Errorf("unable to decode CID: %w", err)
	}

	fetchUrl := c.baseUrl.ResolveReference(&url.URL{Path: fmt.Sprintf("/v1/blobs/%s", ipfsCID)})
	request, err := http.NewRequestWithContext(ctx, "GET", fetchUrl.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request to fetch data from DA: %w", err)
	}
	resp, err := c.httpClient.Do(request)

	if err != nil {
		return nil, fmt.Errorf("unable to fetch DA data: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unable to fetch DA data, got status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read DA data fetch response: %w", err)
	}

	return body, nil
}

func (c DAClient) Write(ctx context.Context, data []byte) ([]byte, error) {
	submitUrl := c.baseUrl.ResolveReference(&url.URL{Path: "/v1/blobs"})

	request, err := http.NewRequestWithContext(ctx, "POST", submitUrl.String(), bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("unable to create request to submit data to DA: %w", err)
	}

	resp, err := c.httpClient.Do(request)

	if err != nil {
		return nil, fmt.Errorf("unable to submit data to DA: %w", err)
	}
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("unable to submit data to DA, got status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read DA data submit response: %w", err)
	}

	var respDto daSubmitResponse
	err = json.Unmarshal(body, &respDto)
	if err != nil {
		return nil, fmt.Errorf("unable to parse DA data submit response json: %w", err)
	}

	if respDto.ID == "" {
		return nil, fmt.Errorf("DA data submit response returned empty ID")
	}

	_, ipfsCID, err := multibase.Decode(respDto.ID)
	if err != nil {
		return nil, fmt.Errorf("DA data submit response returned invalid multibase encoded value: %w", err)
	}

	return ipfsCID, nil
}

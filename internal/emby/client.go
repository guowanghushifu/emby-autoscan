package emby

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func (c Client) RefreshLibrary(ctx context.Context, libraryID string) error {
	requestURL := strings.TrimRight(c.BaseURL, "/") + RefreshPath(libraryID) + "?Recursive=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.APIKey)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("refresh library: unexpected status %s", resp.Status)
	}

	return nil
}

func RefreshPath(libraryID string) string {
	return "/emby/Items/" + url.PathEscape(libraryID) + "/Refresh"
}

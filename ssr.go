package tiktok

import (
	"bytes"
	"encoding/json"
	"fmt"
)

var (
	ssrTagOpen  = []byte(`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">`)
	ssrTagClose = []byte(`</script>`)
)

// extractUniversalData finds and parses the __UNIVERSAL_DATA_FOR_REHYDRATION__
// JSON embedded in TikTok's server-rendered HTML.
func extractUniversalData(htmlBody []byte) (universalData, error) {
	start := bytes.Index(htmlBody, ssrTagOpen)
	if start == -1 {
		return universalData{}, fmt.Errorf("%w: rehydration script tag not found", ErrInvalidResponse)
	}
	start += len(ssrTagOpen)

	end := bytes.Index(htmlBody[start:], ssrTagClose)
	if end == -1 {
		return universalData{}, fmt.Errorf("%w: closing script tag not found", ErrInvalidResponse)
	}

	jsonBytes := htmlBody[start : start+end]

	var data universalData
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return universalData{}, fmt.Errorf("unmarshal ssr data: %w", err)
	}
	return data, nil
}

// extractUserFromSSR pulls the Author from parsed SSR data.
func extractUserFromSSR(data universalData) (Author, error) {
	info := data.DefaultScope.UserDetail.UserInfo
	if info.User.UniqueID == "" {
		return Author{}, fmt.Errorf("%w: user data missing in ssr response", ErrNotFound)
	}
	return parseAuthor(info), nil
}

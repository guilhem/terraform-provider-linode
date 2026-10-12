//go:build unit

package helper_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/linode/terraform-provider-linode/v2/linode/helper"
	"github.com/stretchr/testify/require"
)

func TestClientRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name     string
		header   string
		wantWait time.Duration
		wantErr  string
	}{
		{"beyond maximum", "1", time.Second, ""},
		{"absent", "", 20 * time.Millisecond, ""},
		{"zero", "0", 20 * time.Millisecond, ""},
		{"negative", "-1", 20 * time.Millisecond, ""},
		{"negative duration overflow", "-10000000000", 20 * time.Millisecond, ""},
		{"malformed", "invalid", 0, "strconv.Atoi"},
		{"duration overflow", "9223372036854775807", 0, "Retry-After duration is too large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			requestTimes := make(chan time.Time, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestTimes <- time.Now()
				w.Header().Set("Content-Type", "application/json")
				if requests.Add(1) == 1 {
					if test.header != "" {
						w.Header().Set("Retry-After", test.header)
					}
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprint(w, `{"errors":[{"reason":"Rate limited"}]}`)
					return
				}
				fmt.Fprint(w, `{"id":1,"domain":"example.com"}`)
			}))
			defer server.Close()
			config := helper.Config{
				APIURL:                    server.URL,
				APIVersion:                "v4",
				MinRetryDelayMilliseconds: 20,
				MaxRetryDelayMilliseconds: 20,
			}
			client, err := config.Client(context.Background())
			require.NoError(t, err)
			client.SetRetryCount(1)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			domain, err := client.GetDomain(ctx, 1)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.EqualValues(t, 1, requests.Load(), "invalid headers must stop retries")
			} else {
				require.NoError(t, err)
				require.Equal(t, "example.com", domain.Domain)
				require.EqualValues(t, 2, requests.Load())
				firstRequest := <-requestTimes
				require.GreaterOrEqual(t, (<-requestTimes).Sub(firstRequest), test.wantWait)
			}
		})
	}
}

func TestClientRetryAfterCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"errors":[{"reason":"Rate limited"}]}`)
	}))
	defer server.Close()
	config := helper.Config{
		APIURL:                    server.URL,
		APIVersion:                "v4",
		MinRetryDelayMilliseconds: 1,
		MaxRetryDelayMilliseconds: 2,
	}
	client, err := config.Client(context.Background())
	require.NoError(t, err)
	client.SetRetryCount(2)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = client.R(ctx).Get("domains/1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), time.Second)
	require.EqualValues(t, 1, requests.Load(), "cancellation must stop retries")
}

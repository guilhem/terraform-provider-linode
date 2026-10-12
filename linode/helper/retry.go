package helper

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/linode/linodego"
)

// Workaround for intermittent 5xx errors when retrieving a database from the API
func Database502Retry() func(response *resty.Response, err error) bool {
	databaseGetRegex, err := regexp.Compile("[A-Za-z0-9]+/databases/[a-z]+/instances/[0-9]+")
	if err != nil {
		log.Fatal(err)
	}
	return GenericRetryCondition(500, databaseGetRegex)
}

func LinodeInstance500Retry() func(response *resty.Response, err error) bool {
	linodeGetRegex, err := regexp.Compile("linode/instances/[0-9]+/ips+")
	if err != nil {
		log.Fatal(err)
	}
	return GenericRetryCondition(500, linodeGetRegex)
}

// ImageUpload500Retry for [500] error when uploading an image
func ImageUpload500Retry() func(response *resty.Response, err error) bool {
	ImageUpload, err := regexp.Compile("images/upload")
	if err != nil {
		log.Fatal(err)
	}
	return GenericRetryCondition(500, ImageUpload)
}

// OBJKeyCreate500Retry for [500] error when creating an Object Storage Key
func OBJKeyCreate500Retry() func(response *resty.Response, err error) bool {
	OBJKeyCreate, err := regexp.Compile("object-storage/keys")
	if err != nil {
		log.Fatal(err)
	}
	return GenericRetryCondition(500, OBJKeyCreate)
}

// OBJKeyDelete500Retry for [500] error when deleting an Object Storage Key
func OBJKeyDelete500Retry() func(response *resty.Response, err error) bool {
	OBJKeyDelete, err := regexp.Compile("object-storage/keys/[0-9]+")
	if err != nil {
		log.Fatal(err)
	}
	return GenericRetryCondition(500, OBJKeyDelete)
}

func GenericRetryCondition(statusCode int, pathPattern *regexp.Regexp) func(response *resty.Response, err error) bool {
	return func(response *resty.Response, _ error) bool {
		if response.StatusCode() != statusCode || response.Request == nil {
			return false
		}

		requestURL, err := url.ParseRequestURI(response.Request.URL)
		if err != nil {
			log.Printf("[WARN] failed to parse request URL: %s", err)
			return false
		}

		// Check whether the string matches
		return pathPattern.MatchString(requestURL.Path)
	}
}

func ApplyAllRetryConditions(client *linodego.Client) {
	client.SetRetryAfter(func(_ *resty.Client, resp *resty.Response) (time.Duration, error) {
		header := resp.Header().Get("Retry-After")
		if header == "" {
			return 0, nil
		}
		seconds, err := strconv.Atoi(header)
		if err != nil {
			return 0, err
		}
		if seconds <= 0 {
			return 0, nil
		}
		if int64(seconds) > math.MaxInt64/int64(time.Second) {
			return 0, fmt.Errorf("Retry-After duration is too large: %q", header)
		}
		delay := time.Duration(seconds) * time.Second

		// Wait here to bypass Resty's RetryMaxWaitTime cap, then use its normal backoff.
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-resp.Request.Context().Done():
			return 0, resp.Request.Context().Err()
		case <-timer.C:
			return 0, nil
		}
	})

	client.AddRetryCondition(Database502Retry())
	client.AddRetryCondition(LinodeInstance500Retry())
	client.AddRetryCondition(ImageUpload500Retry())
	client.AddRetryCondition(OBJKeyCreate500Retry())
	client.AddRetryCondition(OBJKeyDelete500Retry())
}

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package oxideclient

import (
	"errors"
	"fmt"

	"github.com/oxidecomputer/oxide.go/oxide"
)

// ShortError collapses an Oxide API error to one line for logs and conditions, e.g.
// `POST /v1/instances?project=demo: 404 ObjectNotFound: not found: disk with name "data" (request 1d16c42c)`.
// The SDK's own Error() dumps the whole HTTP exchange instead. Other errors pass through.
func ShortError(err error) error {
	var herr *oxide.HTTPError
	if !errors.As(err, &herr) || herr.ErrorResponse == nil {
		return err
	}
	// The SDK only builds an HTTPError from a client response, so Request is set.
	res, req, api := herr.HTTPResponse, herr.HTTPResponse.Request, herr.ErrorResponse
	return fmt.Errorf("%s %s: %d %s: %s (request %s)",
		req.Method, req.URL.RequestURI(), res.StatusCode, api.ErrorCode, api.Message, api.RequestId)
}

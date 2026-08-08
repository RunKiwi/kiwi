// Package tunnel implements the client half of the reverse credential proxy:
// the side that runs on the developer's machine, answers secret requests, and
// never sees another tenant's traffic.
//
// The server half lives in ee/tunnel. They are split along the licence
// boundary, not an architectural one: the server terminates connections for
// every tenant and reads auth claims, which makes it part of the multi-tenant
// Control Plane. This half is what the `kiwi` CLI links, so it stays
// Apache-2.0 and pulls in no Control Plane code at all.
package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ConnectAndListen connects to the remote server URL, listens for secret requests,
// looks them up via the getSecret hook, and posts responses back to the server.
func ConnectAndListen(ctx context.Context, serverURL, taskID, authToken string, getSecret func(string) string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := func() error {
			connURL := fmt.Sprintf("%s/tunnel/%s", strings.TrimSuffix(serverURL, "/"), taskID)
			req, err := http.NewRequestWithContext(ctx, "GET", connURL, nil)
			if err != nil {
				return err
			}
			if authToken != "" {
				req.Header.Set("Authorization", "Bearer "+authToken)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status: %s", resp.Status)
			}

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return err // connection dropped, will trigger reconnect
				}
				reqKey := strings.TrimSpace(line)
				if reqKey == "" {
					continue
				}

				secretVal := getSecret(reqKey)

				postURL := fmt.Sprintf("%s/tunnel/%s/response", strings.TrimSuffix(serverURL, "/"), taskID)
				postReq, err := http.NewRequestWithContext(ctx, "POST", postURL, strings.NewReader(secretVal))
				if err != nil {
					return err
				}
				postReq.Header.Set("Content-Type", "text/plain")
				if authToken != "" {
					postReq.Header.Set("Authorization", "Bearer "+authToken)
				}

				postResp, err := http.DefaultClient.Do(postReq)
				if err != nil {
					return err
				}
				postResp.Body.Close()
			}
		}()

		if err != nil {
			// Back off briefly before attempting reconnection
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}
	}
}

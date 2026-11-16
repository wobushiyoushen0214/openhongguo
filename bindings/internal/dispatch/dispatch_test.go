package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beilunyang/openhongguo/adapters/httptransport"
	"github.com/beilunyang/openhongguo/adapters/memstore"
	"github.com/beilunyang/openhongguo/bindings/internal/dispatch"
	"github.com/beilunyang/openhongguo/bindings/internal/fixtures"
	"github.com/beilunyang/openhongguo/sdk"
)

func newClient(t *testing.T, origin string) *sdk.Client {
	t.Helper()
	tr := httptransport.New()
	opts := sdk.Options{Transport: tr, ProfileStore: memstore.New()}
	cfg, err := dispatch.ParseConfig(`{"apiOrigin":"` + origin + `","registerOrigin":"` + origin + `","retryAttempts":1}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Apply(&opts)
	c, err := sdk.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// call 调用 name，并把结果经 JSON 往返解码到 out。
func call(t *testing.T, c *sdk.Client, name, args string, out any) error {
	t.Helper()
	v, err := dispatch.Call(context.Background(), c, name, args)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
	return nil
}

func TestCall(t *testing.T) {
	srv := httptest.NewServer(fixtures.Handler("../../../testdata/protocol"))
	defer srv.Close()
	c := newClient(t, srv.URL)

	var page struct {
		Items []struct{ ID, Title string } `json:"items"`
	}
	if err := call(t, c, "search", `["短剧",{"pageSize":20}]`, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 || page.Items[0].ID != "7100000000000101" {
		t.Fatalf("search = %+v", page)
	}

	var episodes struct {
		Items []struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
		} `json:"items"`
	}
	if err := call(t, c, "listEpisodes", `["7100000000000101"]`, &episodes); err != nil {
		t.Fatal(err)
	}
	if len(episodes.Items) == 0 || episodes.Items[0].Number != 1 {
		t.Fatalf("listEpisodes = %+v", episodes)
	}

	var danmaku struct {
		Items []struct {
			Text     string `json:"text"`
			OffsetMS int64  `json:"offset_ms"`
		} `json:"items"`
		HasMore bool   `json:"has_more"`
		Cursor  string `json:"cursor"`
	}
	if err := call(t, c, "listDanmaku", `["7100000000000101","7100000000000201",{"pageSize":120}]`, &danmaku); err != nil {
		t.Fatal(err)
	}
	if len(danmaku.Items) != 2 || danmaku.Items[0].OffsetMS != 329 || !danmaku.HasMore || danmaku.Cursor == "" {
		t.Fatalf("listDanmaku = %+v", danmaku)
	}

	var pb struct {
		Playback        struct{ Protected bool } `json:"playback"`
		ContentKey      *struct{}                `json:"contentKey"`
		ContentKeyError struct{ Code string }    `json:"contentKeyError"`
	}
	if err := call(t, c, "resolvePlayback", `["7100000000000201"]`, &pb); err != nil {
		t.Fatal(err)
	}
	// 样本里的密钥材料是假的，所以推导失败，但播放模型照常返回。
	if !pb.Playback.Protected || pb.ContentKey != nil || pb.ContentKeyError.Code != "DRM_PROTECTED" {
		t.Fatalf("resolvePlayback = %+v", pb)
	}
}

func TestCallErrors(t *testing.T) {
	srv := httptest.NewServer(fixtures.Handler("../../../testdata/protocol"))
	defer srv.Close()

	tests := []struct {
		name, origin, method, args, code string
		retryable                        bool
	}{
		{"rate limited", srv.URL + "/limited", "search", `["短剧"]`, "RATE_LIMITED", true},
		{"unknown method", srv.URL, "nope", `[]`, "INVALID_OPTIONS", false},
		{"bad arguments", srv.URL, "search", `[1]`, "INVALID_OPTIONS", false},
		{"bad JSON", srv.URL, "search", `{`, "INVALID_OPTIONS", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dispatch.Call(context.Background(), newClient(t, tt.origin), tt.method, tt.args)
			if err == nil {
				t.Fatal("no error")
			}
			e := dispatch.Error(err)
			if e["code"] != tt.code || e["retryable"] != tt.retryable {
				t.Fatalf("Error(%v) = %v", err, e)
			}
		})
	}
}

func TestCallCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := dispatch.Call(ctx, newClient(t, "http://127.0.0.1:1"), "search", `["短剧"]`)
	if got := dispatch.Error(err)["code"]; got != "CANCELLED" {
		t.Fatalf("code = %v (%v)", got, err)
	}
}

func TestParseConfigRejectsBadJSON(t *testing.T) {
	_, err := dispatch.ParseConfig("{")
	if dispatch.Error(err)["code"] != "INVALID_OPTIONS" {
		t.Fatalf("err = %v", err)
	}
}

func TestDeviceResetsPassesThrough(t *testing.T) {
	for raw, want := range map[string]*int{
		`{}`:                 nil,
		`{"deviceResets":0}`: sdk.Int(0),
		`{"deviceResets":3}`: sdk.Int(3),
	} {
		cfg, err := dispatch.ParseConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		var opts sdk.Options
		cfg.Apply(&opts)
		if (want == nil) != (opts.DeviceResets == nil) || (want != nil && *want != *opts.DeviceResets) {
			t.Errorf("%s: DeviceResets = %v, want %v", raw, opts.DeviceResets, want)
		}
	}
}

// 批量入口必须取请求分集的桶，并保留签名、请求体与明确错误。
func TestMultiPlaybackSelectsRequestedBucket(t *testing.T) {
	const id = "7100000000000201"
	const video = `{"status":10,"video_list":{"540p":{"main_url":"https://media.invalid/540.mp4","video_meta":{"definition":"540p","vheight":960}}}}`
	for _, tc := range []struct {
		name, body string
		status     int
		expected   error
	}{
		{"对象模型", `{"code":0,"data":{"7100000000000202":{"video_model":null},"` + id + `":{"video_model":` + video + `}}}`, 200, nil},
		{"字符串模型", `{"code":0,"data":{"` + id + `":{"video_model":` + strconvQuote(video) + `}}}`, 200, nil},
		{"缺请求桶", `{"code":0,"data":{"7100000000000202":{"video_model":` + video + `}}}`, 200, sdk.ErrContentUnavailable},
		{"空响应", "", 200, sdk.ErrAnonymousLimited},
		{"登录拒绝", "", 403, sdk.ErrAuthenticationRequired},
		{"限流", "", 429, sdk.ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/service/2/device_register/" {
					w.Header().Set("Set-Cookie", "ttreq=fixture-ticket; Path=/")
					io.WriteString(w, `{"device_id_str":"7100000000000001","install_id_str":"7100000000000002"}`)
					return
				}
				hits++
				if r.URL.Path != "/novel/player/multi_video_model/v1/" || r.Method != "POST" {
					t.Errorf("请求路径或方法错误")
				}
				raw, _ := io.ReadAll(r.Body)
				var body struct {
					IDs map[string][]string `json:"mixed_video_id_map"`
				}
				json.Unmarshal(raw, &body)
				if strings.Join(body.IDs["1004"], ",") != id {
					t.Errorf("请求分集不一致")
				}
				if len(r.Header.Get("X-Argus")) < 100 || r.Header.Get("X-Medusa") != "" {
					t.Errorf("批量签名错误")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			c, createErr := sdk.NewClient(sdk.Options{Transport: httptransport.New(), ProfileStore: memstore.New(), APIOrigin: srv.URL, RegisterOrigin: srv.URL, RetryAttempts: 1, DeviceResets: sdk.Int(0)})
			if createErr != nil {
				t.Fatal(createErr)
			}
			var pb struct {
				Playback struct {
					EpisodeID string `json:"episode_id"`
					Streams   []struct {
						URL string `json:"url"`
					} `json:"streams"`
				} `json:"playback"`
			}
			err := call(t, c, "resolvePlaybackMulti", `["`+id+`"]`, &pb)
			if tc.expected != nil {
				if !errors.Is(err, tc.expected) {
					t.Fatalf("错误分类不一致：%v", err)
				}
			} else {
				if err != nil || len(pb.Playback.Streams) != 1 || pb.Playback.Streams[0].URL != "https://media.invalid/540.mp4" {
					t.Fatalf("请求模型不一致：%+v，%v", pb, err)
				}
			}
			if hits != 1 {
				t.Fatalf("批量接口只能请求一次，实际%d", hits)
			}
		})
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRegisterLoginPostCommentAndLogout(t *testing.T) {
	server := newTestServer(t)

	resp := requestJSON(t, server, nil, http.MethodPost, "/api/register", `{"username":"alice","displayName":"Alice","password":"pw"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = requestJSON(t, server, nil, http.MethodPost, "/api/login", `{"username":"alice","password":"pw"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	sessionCookie := findCookie(resp, sessionCookieName)
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}
	if sessionCookie.Value != "token-1" {
		t.Fatalf("session token = %q, want token-1", sessionCookie.Value)
	}

	resp = requestJSON(t, server, sessionCookie, http.MethodGet, "/api/me", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp = requestJSON(t, server, sessionCookie, http.MethodPost, "/api/posts", `{"content":"第一則貼文"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create post status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var postResponse struct {
		Post post `json:"post"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postResponse); err != nil {
		t.Fatalf("decode post response: %v", err)
	}

	commentPath := "/api/posts/" + strconv.FormatInt(postResponse.Post.ID, 10) + "/comments"
	resp = requestJSON(t, server, sessionCookie, http.MethodPost, commentPath, `{"content":"第一則留言"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = requestJSON(t, server, sessionCookie, http.MethodGet, "/api/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list posts status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var postsResponse struct {
		Posts []post `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postsResponse); err != nil {
		t.Fatalf("decode posts response: %v", err)
	}
	if len(postsResponse.Posts) != 1 {
		t.Fatalf("posts length = %d, want 1", len(postsResponse.Posts))
	}
	if got := postsResponse.Posts[0].Comments[0].Content; got != "第一則留言" {
		t.Fatalf("comment = %q, want 第一則留言", got)
	}
	if !postsResponse.Posts[0].CanDelete {
		t.Fatal("own post CanDelete = false, want true")
	}
	if !postsResponse.Posts[0].Comments[0].CanDelete {
		t.Fatal("own comment CanDelete = false, want true")
	}

	resp = requestJSON(t, server, sessionCookie, http.MethodPost, "/api/logout", "{}")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp = requestJSON(t, server, sessionCookie, http.MethodGet, "/api/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestBrokenAccessControlDeleteWithoutAuthentication(t *testing.T) {
	server := newTestServer(t)

	alice := registerAndLogin(t, server, "alice", "Alice")

	resp := requestJSON(t, server, alice, http.MethodPost, "/api/posts", `{"content":"Alice 的貼文"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create post status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var postResponse struct {
		Post post `json:"post"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postResponse); err != nil {
		t.Fatalf("decode post response: %v", err)
	}

	commentPath := "/api/posts/" + strconv.FormatInt(postResponse.Post.ID, 10) + "/comments"
	resp = requestJSON(t, server, alice, http.MethodPost, commentPath, `{"content":"Alice 的留言"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create comment status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var commentResponse struct {
		Comment comment `json:"comment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commentResponse); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}

	bob := registerAndLogin(t, server, "bob", "Bob")
	resp = requestJSON(t, server, bob, http.MethodGet, "/api/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list posts as bob status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var bobPostsResponse struct {
		Posts []post `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bobPostsResponse); err != nil {
		t.Fatalf("decode bob posts response: %v", err)
	}
	if len(bobPostsResponse.Posts) != 1 {
		t.Fatalf("bob posts length = %d, want 1", len(bobPostsResponse.Posts))
	}
	if bobPostsResponse.Posts[0].CanDelete {
		t.Fatal("bob sees Alice post CanDelete = true, want false")
	}
	if bobPostsResponse.Posts[0].Comments[0].CanDelete {
		t.Fatal("bob sees Alice comment CanDelete = true, want false")
	}

	resp = requestJSON(t, server, nil, http.MethodDelete, "/api/comments/"+strconv.FormatInt(commentResponse.Comment.ID, 10), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete comment without auth status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp = requestJSON(t, server, nil, http.MethodDelete, "/api/posts/"+strconv.FormatInt(postResponse.Post.ID, 10), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete post without auth status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	resp = requestJSON(t, server, alice, http.MethodGet, "/api/posts", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list posts status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var postsResponse struct {
		Posts []post `json:"posts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postsResponse); err != nil {
		t.Fatalf("decode posts response: %v", err)
	}
	if len(postsResponse.Posts) != 0 {
		t.Fatalf("posts after unauthenticated delete = %+v, want empty", postsResponse.Posts)
	}
}

func TestStorePersistsToJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cyberprep.json")
	store, err := newJSONStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	createdUser, err := store.createUser("alice", "Alice", "pw")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := store.createPost(createdUser, "persistent post"); err != nil {
		t.Fatalf("create post: %v", err)
	}

	reopened, err := newJSONStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	_, ok, err := reopened.findUserByCredentials("alice", "pw")
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if !ok {
		t.Fatal("expected user to persist")
	}
	posts, err := reopened.listPosts(createdUser.ID)
	if err != nil {
		t.Fatalf("list posts: %v", err)
	}
	if len(posts) != 1 || posts[0].Content != "persistent post" {
		t.Fatalf("persisted posts = %+v, want persistent post", posts)
	}
}

func registerAndLogin(t *testing.T, server *httptest.Server, username, displayName string) *http.Cookie {
	t.Helper()

	body := `{"username":"` + username + `","displayName":"` + displayName + `","password":"pw"}`
	resp := requestJSON(t, server, nil, http.MethodPost, "/api/register", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s status = %d, want %d", username, resp.StatusCode, http.StatusCreated)
	}

	resp = requestJSON(t, server, nil, http.MethodPost, "/api/login", `{"username":"`+username+`","password":"pw"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s status = %d, want %d", username, resp.StatusCode, http.StatusOK)
	}
	cookie := findCookie(resp, sessionCookieName)
	if cookie == nil {
		t.Fatalf("login %s did not set session cookie", username)
	}
	return cookie
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	store, err := newJSONStore(filepath.Join(t.TempDir(), "test.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	server := httptest.NewServer((&app{store: store, templates: templates}).routes())
	t.Cleanup(server.Close)
	return server
}

func requestJSON(t *testing.T, server *httptest.Server, cookie *http.Cookie, method, path, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if strings.TrimSpace(body) != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	t.Cleanup(func() {
		resp.Body.Close()
	})
	return resp
}

func findCookie(resp *http.Response, name string) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

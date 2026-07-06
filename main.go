package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAddr       = ":8080"
	dataPath          = "data/cyberprep.json"
	sessionCookieName = "camp26_session"
	sessionDuration   = 24 * time.Hour
	maxContentLength  = 280
)

var (
	errDuplicateUsername = errors.New("duplicate username")
	errNotFound          = errors.New("not found")
	errParentMismatch    = errors.New("parent comment does not belong to post")
)

type app struct {
	store     *jsonStore
	templates *template.Template
}

type user struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type post struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"-"`
	Content           string    `json:"content"`
	CreatedAt         string    `json:"createdAt"`
	AuthorUsername    string    `json:"authorUsername"`
	AuthorDisplayName string    `json:"authorDisplayName"`
	CanDelete         bool      `json:"canDelete"`
	Comments          []comment `json:"comments"`
}

type comment struct {
	ID                int64     `json:"id"`
	PostID            int64     `json:"postID"`
	ParentCommentID   *int64    `json:"parentCommentID,omitempty"`
	UserID            int64     `json:"-"`
	Content           string    `json:"content"`
	CreatedAt         string    `json:"createdAt"`
	AuthorUsername    string    `json:"authorUsername"`
	AuthorDisplayName string    `json:"authorDisplayName"`
	CanDelete         bool      `json:"canDelete"`
	Replies           []comment `json:"replies"`
}

type registerRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type createPostRequest struct {
	Content string `json:"content"`
}

type createCommentRequest struct {
	Content         string `json:"content"`
	ParentCommentID *int64 `json:"parentCommentID"`
}

type storeData struct {
	NextUserID    int64           `json:"nextUserID"`
	NextPostID    int64           `json:"nextPostID"`
	NextCommentID int64           `json:"nextCommentID"`
	NextSessionID int64           `json:"nextSessionID"`
	Users         []userRecord    `json:"users"`
	Sessions      []sessionRecord `json:"sessions"`
	Posts         []postRecord    `json:"posts"`
	Comments      []commentRecord `json:"comments"`
}

type userRecord struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	CreatedAt   string `json:"createdAt"`
}

type sessionRecord struct {
	Token     string `json:"token"`
	UserID    int64  `json:"userID"`
	ExpiresAt string `json:"expiresAt"`
}

type postRecord struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userID"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

type commentRecord struct {
	ID              int64  `json:"id"`
	PostID          int64  `json:"postID"`
	ParentCommentID *int64 `json:"parentCommentID,omitempty"`
	UserID          int64  `json:"userID"`
	Content         string `json:"content"`
	CreatedAt       string `json:"createdAt"`
}

type jsonStore struct {
	mu   sync.Mutex
	path string
	data storeData
}

func main() {
	store, err := newJSONStore(dataPath)
	if err != nil {
		log.Fatalf("open data store: %v", err)
	}

	templates, err := loadTemplates()
	if err != nil {
		log.Fatalf("load templates: %v", err)
	}

	a := &app{store: store, templates: templates}
	addr := listenAddr()
	log.Printf("listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, a.routes()); err != nil {
		log.Fatal(err)
	}
}

func listenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if strings.HasPrefix(port, ":") {
			return port
		}
		return ":" + port
	}
	return defaultAddr
}

func loadTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"initial": initial,
		"year": func() int {
			return time.Now().Year()
		},
	}).ParseGlob("templates/*.html")
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("GET /register", a.registerPage)
	mux.HandleFunc("GET /app", a.appPage)
	mux.HandleFunc("POST /api/register", a.register)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.logout)
	mux.HandleFunc("GET /api/me", a.me)
	mux.HandleFunc("GET /api/posts", a.listPosts)
	mux.HandleFunc("POST /api/posts", a.createPost)
	mux.HandleFunc("DELETE /api/posts/{postID}", a.deletePost)
	mux.HandleFunc("POST /api/posts/{postID}/comments", a.createComment)
	mux.HandleFunc("DELETE /api/comments/{commentID}", a.deleteComment)
	return mux
}

func newJSONStore(path string) (*jsonStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	store := &jsonStore{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		store.data = storeData{}
		store.normalizeLocked()
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &store.data); err != nil {
			return nil, err
		}
	}
	store.normalizeLocked()
	return store, nil
}

func (s *jsonStore) normalizeLocked() {
	if s.data.NextUserID < 1 {
		s.data.NextUserID = 1
	}
	if s.data.NextPostID < 1 {
		s.data.NextPostID = 1
	}
	if s.data.NextCommentID < 1 {
		s.data.NextCommentID = 1
	}
	if s.data.NextSessionID < 1 {
		s.data.NextSessionID = 1
	}
	for _, u := range s.data.Users {
		if u.ID >= s.data.NextUserID {
			s.data.NextUserID = u.ID + 1
		}
	}
	for _, p := range s.data.Posts {
		if p.ID >= s.data.NextPostID {
			s.data.NextPostID = p.ID + 1
		}
	}
	for _, c := range s.data.Comments {
		if c.ID >= s.data.NextCommentID {
			s.data.NextCommentID = c.ID + 1
		}
	}
	for _, session := range s.data.Sessions {
		n, ok := strings.CutPrefix(session.Token, "token-")
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(n, 10, 64)
		if err == nil && id >= s.data.NextSessionID {
			s.data.NextSessionID = id + 1
		}
	}
}

func (s *jsonStore) saveLocked() error {
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *jsonStore) createUser(username, displayName, password string) (user, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.data.Users {
		if strings.EqualFold(existing.Username, username) {
			return user{}, errDuplicateUsername
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	record := userRecord{
		ID:          s.data.NextUserID,
		Username:    username,
		DisplayName: displayName,
		Password:    password,
		CreatedAt:   now,
	}
	s.data.NextUserID++
	s.data.Users = append(s.data.Users, record)
	if err := s.saveLocked(); err != nil {
		return user{}, err
	}
	return publicUser(record), nil
}

func (s *jsonStore) findUserByCredentials(username, password string) (user, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range s.data.Users {
		if record.Username == username && record.Password == password {
			return publicUser(record), true, nil
		}
	}
	return user{}, false, nil
}

func (s *jsonStore) findUserBySession(token string) (user, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, session := range s.data.Sessions {
		if session.Token != token {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
		if err != nil {
			return user{}, false, err
		}
		if time.Now().UTC().After(expiresAt) {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			_ = s.saveLocked()
			return user{}, false, nil
		}
		for _, record := range s.data.Users {
			if record.ID == session.UserID {
				return publicUser(record), true, nil
			}
		}
		return user{}, false, nil
	}
	return user{}, false, nil
}

func (s *jsonStore) createSession(userID int64) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := fmt.Sprintf("token-%d", s.data.NextSessionID)
	s.data.NextSessionID++
	expiresAt := time.Now().UTC().Add(sessionDuration)
	s.data.Sessions = append(s.data.Sessions, sessionRecord{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	})
	if err := s.saveLocked(); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *jsonStore) deleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, session := range s.data.Sessions {
		if session.Token == token {
			s.data.Sessions = append(s.data.Sessions[:i], s.data.Sessions[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *jsonStore) listPosts(currentUserID int64) ([]post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	users := map[int64]userRecord{}
	for _, u := range s.data.Users {
		users[u.ID] = u
	}

	records := append([]postRecord(nil), s.data.Posts...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
	if len(records) > 100 {
		records = records[:100]
	}

	postIDs := map[int64]struct{}{}
	for _, record := range records {
		postIDs[record.ID] = struct{}{}
	}
	commentsByPost := buildComments(s.data.Comments, users, postIDs, currentUserID)

	posts := make([]post, 0, len(records))
	for _, record := range records {
		author := users[record.UserID]
		item := post{
			ID:                record.ID,
			UserID:            record.UserID,
			Content:           record.Content,
			CreatedAt:         record.CreatedAt,
			AuthorUsername:    author.Username,
			AuthorDisplayName: author.DisplayName,
			CanDelete:         true,
			Comments:          commentsByPost[record.ID],
		}
		if item.Comments == nil {
			item.Comments = []comment{}
		}
		posts = append(posts, item)
	}
	return posts, nil
}

func (s *jsonStore) createPost(currentUser user, content string) (post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	record := postRecord{
		ID:        s.data.NextPostID,
		UserID:    currentUser.ID,
		Content:   content,
		CreatedAt: now,
	}
	s.data.NextPostID++
	s.data.Posts = append(s.data.Posts, record)
	if err := s.saveLocked(); err != nil {
		return post{}, err
	}

	return post{
		ID:                record.ID,
		UserID:            currentUser.ID,
		Content:           content,
		CreatedAt:         now,
		AuthorUsername:    currentUser.Username,
		AuthorDisplayName: currentUser.DisplayName,
		CanDelete:         true,
		Comments:          []comment{},
	}, nil
}

func (s *jsonStore) createComment(currentUser user, postID int64, parentID *int64, content string) (comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !postExists(s.data.Posts, postID) {
		return comment{}, errNotFound
	}
	if parentID != nil && !commentBelongsToPost(s.data.Comments, *parentID, postID) {
		return comment{}, errParentMismatch
	}

	now := time.Now().UTC().Format(time.RFC3339)
	record := commentRecord{
		ID:              s.data.NextCommentID,
		PostID:          postID,
		ParentCommentID: parentID,
		UserID:          currentUser.ID,
		Content:         content,
		CreatedAt:       now,
	}
	s.data.NextCommentID++
	s.data.Comments = append(s.data.Comments, record)
	if err := s.saveLocked(); err != nil {
		return comment{}, err
	}

	return comment{
		ID:                record.ID,
		PostID:            postID,
		ParentCommentID:   parentID,
		UserID:            currentUser.ID,
		Content:           content,
		CreatedAt:         now,
		AuthorUsername:    currentUser.Username,
		AuthorDisplayName: currentUser.DisplayName,
		CanDelete:         true,
		Replies:           []comment{},
	}, nil
}

func (s *jsonStore) deletePost(postID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := -1
	for i, record := range s.data.Posts {
		if record.ID == postID {
			index = i
			break
		}
	}
	if index < 0 {
		return errNotFound
	}

	s.data.Posts = append(s.data.Posts[:index], s.data.Posts[index+1:]...)
	kept := s.data.Comments[:0]
	for _, record := range s.data.Comments {
		if record.PostID != postID {
			kept = append(kept, record)
		}
	}
	s.data.Comments = kept
	return s.saveLocked()
}

func (s *jsonStore) deleteComment(commentID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	for _, record := range s.data.Comments {
		if record.ID == commentID {
			found = true
			break
		}
	}
	if !found {
		return errNotFound
	}

	toDelete := map[int64]struct{}{commentID: {}}
	changed := true
	for changed {
		changed = false
		for _, record := range s.data.Comments {
			if record.ParentCommentID == nil {
				continue
			}
			if _, ok := toDelete[*record.ParentCommentID]; ok {
				if _, already := toDelete[record.ID]; !already {
					toDelete[record.ID] = struct{}{}
					changed = true
				}
			}
		}
	}

	kept := s.data.Comments[:0]
	for _, record := range s.data.Comments {
		if _, ok := toDelete[record.ID]; !ok {
			kept = append(kept, record)
		}
	}
	s.data.Comments = kept
	return s.saveLocked()
}

func publicUser(record userRecord) user {
	return user{
		ID:          record.ID,
		Username:    record.Username,
		DisplayName: record.DisplayName,
		CreatedAt:   record.CreatedAt,
	}
}

func postExists(posts []postRecord, postID int64) bool {
	for _, record := range posts {
		if record.ID == postID {
			return true
		}
	}
	return false
}

func commentBelongsToPost(comments []commentRecord, commentID, postID int64) bool {
	for _, record := range comments {
		if record.ID == commentID {
			return record.PostID == postID
		}
	}
	return false
}

func buildComments(records []commentRecord, users map[int64]userRecord, postIDs map[int64]struct{}, currentUserID int64) map[int64][]comment {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			return records[i].ID < records[j].ID
		}
		return records[i].CreatedAt < records[j].CreatedAt
	})

	nodes := map[int64]*comment{}
	ordered := []*comment{}
	for _, record := range records {
		if _, ok := postIDs[record.PostID]; !ok {
			continue
		}
		author := users[record.UserID]
		node := &comment{
			ID:                record.ID,
			PostID:            record.PostID,
			UserID:            record.UserID,
			Content:           record.Content,
			CreatedAt:         record.CreatedAt,
			AuthorUsername:    author.Username,
			AuthorDisplayName: author.DisplayName,
			CanDelete:         true,
			Replies:           []comment{},
		}
		if record.ParentCommentID != nil {
			parentID := *record.ParentCommentID
			node.ParentCommentID = &parentID
		}
		nodes[node.ID] = node
		ordered = append(ordered, node)
	}

	children := map[int64][]*comment{}
	roots := map[int64][]*comment{}
	for _, node := range ordered {
		if node.ParentCommentID != nil {
			if parent, ok := nodes[*node.ParentCommentID]; ok {
				children[parent.ID] = append(children[parent.ID], node)
				continue
			}
		}
		roots[node.PostID] = append(roots[node.PostID], node)
	}

	result := map[int64][]comment{}
	for postID, rootNodes := range roots {
		result[postID] = commentValues(rootNodes, children)
	}
	return result
}

func commentValues(nodes []*comment, children map[int64][]*comment) []comment {
	result := make([]comment, 0, len(nodes))
	for _, node := range nodes {
		item := *node
		item.Replies = commentValues(children[node.ID], children)
		result = append(result, item)
	}
	if result == nil {
		return []comment{}
	}
	return result
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", nil)
}

func (a *app) registerPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); ok {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	a.render(w, "register.html", nil)
}

func (a *app) appPage(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	a.render(w, "app.html", currentUser)
}

func (a *app) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式不正確")
		return
	}

	req.Username = normalizeUsername(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Password = strings.TrimSpace(req.Password)
	if !validUsername(req.Username) {
		writeError(w, http.StatusBadRequest, "帳號只能使用 3-24 個小寫英數字、底線或連字號")
		return
	}
	if req.DisplayName == "" || len([]rune(req.DisplayName)) > 40 {
		writeError(w, http.StatusBadRequest, "顯示名稱需要 1-40 個字")
		return
	}
	if req.Password == "" || len([]rune(req.Password)) > 72 {
		writeError(w, http.StatusBadRequest, "密碼需要 1-72 個字")
		return
	}

	createdUser, err := a.store.createUser(req.Username, req.DisplayName, req.Password)
	if errors.Is(err, errDuplicateUsername) {
		writeError(w, http.StatusConflict, "這個帳號已經被註冊")
		return
	}
	if err != nil {
		log.Printf("create user: %v", err)
		writeError(w, http.StatusInternalServerError, "無法建立帳號")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": createdUser})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式不正確")
		return
	}

	req.Username = normalizeUsername(req.Username)
	req.Password = strings.TrimSpace(req.Password)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "帳號與密碼都要填")
		return
	}

	currentUser, ok, err := a.store.findUserByCredentials(req.Username, req.Password)
	if err != nil {
		log.Printf("find user: %v", err)
		writeError(w, http.StatusInternalServerError, "無法登入")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "帳號或密碼不正確")
		return
	}

	token, expiresAt, err := a.store.createSession(currentUser.ID)
	if err != nil {
		log.Printf("create session: %v", err)
		writeError(w, http.StatusInternalServerError, "無法建立 session")
		return
	}

	// This is intentionally readable by JavaScript for the classroom demo.
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookieName,
		Value:   token,
		Path:    "/",
		MaxAge:  int(sessionDuration.Seconds()),
		Expires: expiresAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser})
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
		if err := a.store.deleteSession(cookie.Value); err != nil {
			log.Printf("delete session: %v", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookieName,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要先登入")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser})
}

func (a *app) listPosts(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要先登入")
		return
	}

	posts, err := a.store.listPosts(currentUser.ID)
	if err != nil {
		log.Printf("list posts: %v", err)
		writeError(w, http.StatusInternalServerError, "無法讀取貼文")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (a *app) createPost(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要先登入")
		return
	}

	var req createPostRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式不正確")
		return
	}
	content, ok := cleanContent(req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "內容需要 1-280 個字")
		return
	}

	createdPost, err := a.store.createPost(currentUser, content)
	if err != nil {
		log.Printf("create post: %v", err)
		writeError(w, http.StatusInternalServerError, "無法新增貼文")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"post": createdPost})
}

func (a *app) createComment(w http.ResponseWriter, r *http.Request) {
	currentUser, ok := a.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "需要先登入")
		return
	}

	postID, ok := pathID(w, r, "postID")
	if !ok {
		return
	}

	var req createCommentRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式不正確")
		return
	}
	content, ok := cleanContent(req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "內容需要 1-280 個字")
		return
	}

	createdComment, err := a.store.createComment(currentUser, postID, req.ParentCommentID, content)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到貼文")
		return
	}
	if errors.Is(err, errParentMismatch) {
		writeError(w, http.StatusBadRequest, "回覆目標不屬於這則貼文")
		return
	}
	if err != nil {
		log.Printf("create comment: %v", err)
		writeError(w, http.StatusInternalServerError, "無法新增留言")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"comment": createdComment})
}

func (a *app) deletePost(w http.ResponseWriter, r *http.Request) {
	postID, ok := pathID(w, r, "postID")
	if !ok {
		return
	}

	err := a.store.deletePost(postID)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到貼文")
		return
	}
	if err != nil {
		log.Printf("delete post: %v", err)
		writeError(w, http.StatusInternalServerError, "無法刪除貼文")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) deleteComment(w http.ResponseWriter, r *http.Request) {
	commentID, ok := pathID(w, r, "commentID")
	if !ok {
		return
	}

	err := a.store.deleteComment(commentID)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "找不到留言")
		return
	}
	if err != nil {
		log.Printf("delete comment: %v", err)
		writeError(w, http.StatusInternalServerError, "無法刪除留言")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) currentUser(r *http.Request) (user, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return user{}, false
	}
	currentUser, ok, err := a.store.findUserBySession(cookie.Value)
	if err != nil {
		log.Printf("find user by session: %v", err)
		return user{}, false
	}
	return currentUser, ok
}

func normalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 24 {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func cleanContent(value string) (string, bool) {
	content := strings.TrimSpace(value)
	if content == "" || len([]rune(content)) > maxContentLength {
		return "", false
	}
	return content, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "路徑 ID 不正確")
		return 0, false
	}
	return id, true
}

func readJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (a *app) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func initial(value string) string {
	value = strings.TrimSpace(value)
	for _, r := range value {
		return string(r)
	}
	return "?"
}

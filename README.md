# Camp 2026 Cyberprep Lab

這是一個資安先導課程用的極簡 Web demo。它不是正式社群產品，而是讓學員可以用瀏覽器 DevTools 觀察：

- HTML / CSS / JavaScript 如何被瀏覽器載入
- 註冊、登入、發文、留言時的 HTTP request 與 JSON payload
- 伺服器如何用 Cookie 找到 Session
- Session Cookie 被複製後，為什麼可能被冒用
- 前端送來的資料為什麼仍然要由後端重新驗證

## 功能

- 註冊、登入、登出
- 登入後顯示目前使用者
- 發布 280 字內文字貼文
- 回覆貼文與回覆留言
- 刪除自己畫面上可見的貼文與留言
- 所有登入使用者共用同一條時間線
- 資料持久化到 `data/cyberprep.json`

## 執行

```sh
go run .
```

預設網址：

```text
http://localhost:8080
```

如果要換 port：

```sh
PORT=8081 go run .
```

## Docker Compose

```sh
docker compose up --build
```

服務會在容器內使用 `PORT=8080`，資料會保存在 compose volume `cyberprep-data`。

## 課堂操作

1. 開啟 `http://localhost:8080`
2. 開 DevTools，切到 Network
3. 註冊一個帳號，觀察 `/api/register`
4. 登入帳號，觀察 `/api/login` 的 `Set-Cookie`
5. 進入 Application / Storage，找到 `camp26_session`
6. 發文或留言，觀察 request payload 與 cookie header
7. 兩個帳號互換 `camp26_session` 值，觀察伺服器如何判斷目前使用者

## 刻意保留的不安全設計

這些設計是課堂示範用，不應該放進正式服務：

- 密碼明文存放在 `data/cyberprep.json`
- Session token 是可預測的 `token-1`、`token-2`
- Cookie 沒有設定 `HttpOnly`，所以前端 JavaScript 可以讀到
- 沒有 CSRF 防護
- 貼文與留言內容會被當成 HTML 插入頁面，因此預設可以示範 stored XSS
- `DELETE /api/posts/{postID}` 與 `DELETE /api/comments/{commentID}` 不需要登入，也不檢查資源擁有者，因此可以示範 broken access control
- 沒有 rate limit
- 沒有 HTTPS
- 沒有正式資料庫與 migration

正式服務至少要改成密碼雜湊、不可預測的高熵 session token、合適的 Cookie 屬性、HTTPS、CSRF 防護與更完整的授權檢查。

## XSS Lab

這個 demo 預設會刻意用 `innerHTML` 顯示貼文與留言內容，讓課堂可以示範「一個人發了惡意貼文，其他登入使用者載入時間線時，瀏覽器自動用自己的 session 發出 request」。

只在課堂 demo 帳號使用，不要拿真實帳號或其他網站操作。下面 payload 會在受害者第一次載入該貼文時，用受害者自己的 session 發一篇貼文：

```html
<img src=x onerror="if(!sessionStorage.x){sessionStorage.x=1;fetch('/api/posts',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({content:'XSS demo: this post used my session'})})}">
```

如果想確認 HTML 會被解析，可以先發 `<h1>Test</h1>`。它應該會變成標題，而不是顯示原始文字。

## Broken Access Control Lab

刪除 API 是刻意做壞的：不需要 cookie，也不確認目前使用者是不是作者。前端只會對目前使用者自己的貼文與留言顯示刪除按鈕，但知道任何貼文或留言 ID 的人，都可以直接呼叫 API 刪除。

```sh
curl -X DELETE http://localhost:8080/api/posts/1
curl -X DELETE http://localhost:8080/api/comments/1
```

課堂上可以讓學員先用 DevTools 觀察刪除 request，再把 Cookie header 移除重送一次，對比「登入驗證」和「授權檢查」不是同一件事。

## API

| Method | Path | 功能 |
| --- | --- | --- |
| `POST` | `/api/register` | 建立帳號 |
| `POST` | `/api/login` | 登入並設定 cookie |
| `POST` | `/api/logout` | 登出並清除 session |
| `GET` | `/api/me` | 取得目前使用者 |
| `GET` | `/api/posts` | 取得時間線 |
| `POST` | `/api/posts` | 新增貼文 |
| `DELETE` | `/api/posts/{postID}` | 刪除貼文，刻意不需要登入 |
| `POST` | `/api/posts/{postID}/comments` | 新增留言或回覆 |
| `DELETE` | `/api/comments/{commentID}` | 刪除留言與底下回覆，刻意不需要登入 |

## 測試

```sh
go test ./...
```

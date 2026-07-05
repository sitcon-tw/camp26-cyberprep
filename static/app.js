const page = document.body.dataset.page;

async function api(path, options = {}) {
  const response = await fetch(path, {
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(data.error || "request failed");
  }
  return data;
}

function setMessage(element, text, type = "error") {
  if (!element) return;
  element.textContent = text;
  element.dataset.type = type;
}

function formJSON(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function icon(name) {
  return `<svg aria-hidden="true"><use href="/static/icons.svg#${name}"></use></svg>`;
}

function firstChar(value) {
  return Array.from(value || "?")[0] || "?";
}

function formatTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

if (page === "login") {
  const form = document.querySelector("#login-form");
  const message = document.querySelector("#form-message");

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    setMessage(message, "");

    try {
      await api("/api/login", {
        method: "POST",
        body: JSON.stringify(formJSON(form)),
      });
      window.location.href = "/app";
    } catch (error) {
      setMessage(message, error.message);
    }
  });
}

if (page === "register") {
  const form = document.querySelector("#register-form");
  const message = document.querySelector("#form-message");

  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    setMessage(message, "");

    try {
      await api("/api/register", {
        method: "POST",
        body: JSON.stringify(formJSON(form)),
      });
      setMessage(message, "帳號已建立，請登入。", "success");
      form.reset();
      window.setTimeout(() => {
        window.location.href = "/login";
      }, 650);
    } catch (error) {
      setMessage(message, error.message);
    }
  });
}

if (page === "app") {
  const postForm = document.querySelector("#post-form");
  const postMessage = document.querySelector("#post-message");
  const postsElement = document.querySelector("#posts");
  const textarea = postForm.querySelector("textarea");
  const charCount = document.querySelector("#char-count");
  const refreshButton = document.querySelector("#refresh-button");
  const logoutButton = document.querySelector("#logout-button");
  let activeReplyForm = null;

  refreshButton.addEventListener("click", loadPosts);

  logoutButton.addEventListener("click", async () => {
    await api("/api/logout", {
      method: "POST",
      body: "{}",
    });
    window.location.href = "/login";
  });

  textarea.addEventListener("input", () => {
    charCount.textContent = `${Array.from(textarea.value).length} / 280`;
  });

  postForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    setMessage(postMessage, "");

    try {
      await api("/api/posts", {
        method: "POST",
        body: JSON.stringify(formJSON(postForm)),
      });
      postForm.reset();
      charCount.textContent = "0 / 280";
      await loadPosts();
    } catch (error) {
      setMessage(postMessage, error.message);
    }
  });

  loadPosts();

  async function loadPosts() {
    try {
      const data = await api("/api/posts");
      const posts = data.posts || [];
      renderPosts(posts);
    } catch (error) {
      setMessage(postMessage, error.message);
    }
  }

  function renderPosts(posts) {
    postsElement.replaceChildren();
    activeReplyForm = null;

    if (posts.length === 0) {
      const empty = document.createElement("section");
      empty.className = "empty-state";
      empty.textContent = "還沒有貼文。";
      postsElement.append(empty);
      return;
    }

    for (const post of posts) {
      postsElement.append(renderPost(post));
    }
  }

  function renderPost(post) {
    const article = document.createElement("article");
    article.className = "post";

    const avatar = document.createElement("span");
    avatar.className = "avatar";
    avatar.textContent = firstChar(post.authorDisplayName);

    const body = document.createElement("div");
    body.className = "post-body";

    body.append(
      renderMeta(post.authorDisplayName, post.authorUsername, post.createdAt),
      renderContent(post.content),
      renderActions([
        action("回覆", "message", () => showReplyForm(body, post.id)),
        post.canDelete ? action("刪除", "trash", () => deletePost(post.id), "danger") : null,
      ]),
    );

    if (post.comments?.length) {
      body.append(renderComments(post.comments, post.id));
    }
    article.append(avatar, body);
    return article;
  }

  function renderComments(comments, postID) {
    const list = document.createElement("section");
    list.className = "comments";
    for (const comment of comments) {
      list.append(renderComment(comment, postID));
    }
    return list;
  }

  function renderComment(comment, postID) {
    const article = document.createElement("article");
    article.className = "comment";

    const avatar = document.createElement("span");
    avatar.className = "avatar small";
    avatar.textContent = firstChar(comment.authorDisplayName);

    const body = document.createElement("div");
    body.className = "comment-body";
    body.append(
      renderMeta(comment.authorDisplayName, comment.authorUsername, comment.createdAt),
      renderContent(comment.content),
      renderActions([
        action("回覆", "message", () => showReplyForm(body, postID, comment.id)),
        comment.canDelete ? action("刪除", "trash", () => deleteComment(comment.id), "danger") : null,
      ]),
    );

    if (comment.replies?.length) {
      body.append(renderComments(comment.replies, postID));
    }
    article.append(avatar, body);
    return article;
  }

  function renderMeta(displayName, username, createdAt) {
    const meta = document.createElement("div");
    meta.className = "post-meta";

    const name = document.createElement("strong");
    name.textContent = displayName;

    const handle = document.createElement("span");
    handle.textContent = `@${username}`;

    const time = document.createElement("time");
    time.dateTime = createdAt;
    time.textContent = formatTime(createdAt);

    meta.append(name, handle, time);
    return meta;
  }

  function renderContent(value) {
    const content = document.createElement("p");
    content.className = "post-content";
    content.innerHTML = value;
    return content;
  }

  function renderActions(actions) {
    const list = document.createElement("div");
    list.className = "post-actions";
    for (const item of actions) {
      if (item) list.append(item);
    }
    return list;
  }

  function action(label, iconName, onClick, tone = "") {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `action-button ${tone}`.trim();
    button.setAttribute("aria-label", label);
    button.title = label;
    button.innerHTML = icon(iconName);
    button.addEventListener("click", onClick);
    return button;
  }

  function showReplyForm(container, postID, parentCommentID) {
    if (activeReplyForm) {
      activeReplyForm.remove();
      activeReplyForm = null;
    }

    const form = document.createElement("form");
    form.className = "reply-form";

    const input = document.createElement("textarea");
    input.name = "content";
    input.rows = 2;
    input.maxLength = 280;
    input.placeholder = "寫下回覆...";
    input.required = true;

    const message = document.createElement("p");
    message.className = "message";
    message.setAttribute("role", "status");

    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "ghost-button compact";
    cancel.innerHTML = `${icon("x")}取消`;
    cancel.addEventListener("click", () => {
      form.remove();
      activeReplyForm = null;
    });

    const submit = document.createElement("button");
    submit.type = "submit";
    submit.className = "primary-button compact";
    submit.innerHTML = `${icon("send")}回覆`;

    const actions = document.createElement("div");
    actions.className = "reply-actions";
    actions.append(message, cancel, submit);

    form.append(input, actions);
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      setMessage(message, "");

      try {
        await api(`/api/posts/${postID}/comments`, {
          method: "POST",
          body: JSON.stringify({
            content: input.value,
            parentCommentID,
          }),
        });
        await loadPosts();
      } catch (error) {
        setMessage(message, error.message);
      }
    });

    container.append(form);
    activeReplyForm = form;
    input.focus();
  }

  async function deletePost(postID) {
    if (!window.confirm("刪除這則貼文？")) return;
    try {
      await api(`/api/posts/${postID}`, { method: "DELETE" });
      await loadPosts();
    } catch (error) {
      setMessage(postMessage, error.message);
    }
  }

  async function deleteComment(commentID) {
    if (!window.confirm("刪除這則留言與底下回覆？")) return;
    try {
      await api(`/api/comments/${commentID}`, { method: "DELETE" });
      await loadPosts();
    } catch (error) {
      setMessage(postMessage, error.message);
    }
  }
}

(function () {
  var $ = function (id) { return document.getElementById(id); };
  var setup = $("setup");
  var compose = $("compose");
  var history = $("history");
  var info = { token_required: false, max_image_bytes: 12 * 1024 * 1024 };
  var file = null;
  var previewURL = "";
  var nextBefore = null;
  var sending = false;

  function channel() { return localStorage.getItem("cn.channel") || ""; }
  function token() { return localStorage.getItem("cn.token") || ""; }

  function show(name) {
    setup.hidden = name !== "setup";
    compose.hidden = name !== "compose";
    history.hidden = name !== "history";
    if (name === "setup") {
      $("hero-title").textContent = "发给讲台电脑";
      $("hero-sub").textContent = "输入电脑上显示的教室编号";
    } else if (name === "compose") {
      $("hero-title").textContent = "教室 " + channel();
      $("hero-sub").textContent = "写一句话，或发一张照片";
    } else {
      $("hero-title").textContent = "历史消息";
      $("hero-sub").textContent = "教室 " + channel();
    }
    window.scrollTo(0, 0);
  }

  function showTokenField(force) {
    var needed = info.token_required || force || !!token();
    $("token-block").hidden = !needed;
    $("show-token").hidden = needed;
  }

  function applyInfo() {
    if (info.token_required) {
      $("token-label").textContent = "口令";
      $("token").placeholder = "和电脑上填的一样";
      $("token-help").textContent = "这台服务器需要口令，只用英文字母或数字。";
      $("channel").enterKeyHint = "next";
    } else {
      $("token-label").textContent = "口令（可不填）";
      $("token").placeholder = "服务器没设口令就留空";
      $("token-help").textContent = "当前只用教室编号配对。";
      $("channel").enterKeyHint = "go";
    }
    showTokenField(false);
  }

  async function loadInfo() {
    try {
      var res = await fetch("/api/v1/info", { cache: "no-store" });
      if (!res.ok) throw new Error("bad");
      info = await res.json();
    } catch (e) {
      $("setup-error").textContent = "网页开了，但没有连上服务，请稍后再试";
    }
    applyInfo();
  }

  async function enterRoom() {
    var ch = $("channel").value.trim();
    var tk = $("token").value.trim();
    $("setup-error").textContent = "";
    if (!ch) {
      $("setup-error").textContent = "请填写教室编号";
      return;
    }
    $("enter").disabled = true;
    $("enter").textContent = "正在进入…";
    try {
      var res = await fetch("/api/v1/pair", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ channel: ch, token: tk })
      });
      var data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || "进入失败");
      localStorage.setItem("cn.channel", data.channel || ch);
      localStorage.setItem("cn.token", tk);
      show("compose");
    } catch (err) {
      $("setup-error").textContent = err.message || "进入失败";
    } finally {
      $("enter").disabled = false;
      $("enter").textContent = "进入教室";
    }
  }

  function setStatus(text, bad) {
    var el = $("send-status");
    el.textContent = text || "";
    el.classList.toggle("bad", !!bad);
  }

  function clearPhoto() {
    file = null;
    if (previewURL) URL.revokeObjectURL(previewURL);
    previewURL = "";
    $("file").value = "";
    $("preview").hidden = true;
    $("preview").removeAttribute("src");
    $("clear-photo").hidden = true;
    $("pick").textContent = "添加照片";
  }

  async function send() {
    if (sending) return;
    var text = $("text").value.trim();
    if (!text && !file) {
      setStatus("先写一句话，或添加一张照片", true);
      return;
    }
    if (file && info.max_image_bytes && file.size > info.max_image_bytes) {
      var mb = Math.max(1, Math.round(info.max_image_bytes / (1024 * 1024)));
      setStatus("照片太大，最大 " + mb + "MB", true);
      return;
    }
    sending = true;
    $("send").disabled = true;
    $("send").textContent = "发送中…";
    setStatus("");
    try {
      var fd = new FormData();
      fd.set("channel", channel());
      fd.set("token", token());
      fd.set("body", text);
      if (file) fd.set("image", file, file.name || "photo.jpg");
      var res = await fetch("/api/v1/messages", {
        method: "POST",
        body: fd,
        credentials: "same-origin"
      });
      var data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || "发送失败");
      $("text").value = "";
      clearPhoto();
      setStatus("已发送到讲台电脑", false);
    } catch (err) {
      setStatus(err.message || "发送失败", true);
    } finally {
      sending = false;
      $("send").disabled = false;
      $("send").textContent = "发送";
    }
  }

  function fileURL(id) {
    var q = new URLSearchParams();
    if (channel()) q.set("channel", channel());
    if (token()) q.set("token", token());
    var s = q.toString();
    return "/api/v1/files/" + encodeURIComponent(id) + (s ? "?" + s : "");
  }

  function fmt(iso) {
    var d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    var now = new Date();
    var hh = String(d.getHours()).padStart(2, "0");
    var mm = String(d.getMinutes()).padStart(2, "0");
    var same = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
    if (same) return "今天 " + hh + ":" + mm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日 " + hh + ":" + mm;
  }

  function appendMessages(messages, reset) {
    var list = $("list");
    if (reset) list.replaceChildren();
    messages.forEach(function (m) {
      var li = document.createElement("li");
      li.className = "item";
      var time = document.createElement("time");
      time.textContent = fmt(m.created_at);
      time.dateTime = m.created_at || "";
      li.appendChild(time);
      if (m.body) {
        var p = document.createElement("p");
        p.textContent = m.body;
        li.appendChild(p);
      }
      if (m.has_image) {
        var img = document.createElement("img");
        img.alt = "照片";
        img.src = fileURL(m.id);
        li.appendChild(img);
      } else if (!m.body) {
        var empty = document.createElement("p");
        empty.textContent = "（空消息）";
        li.appendChild(empty);
      }
      list.appendChild(li);
    });
    $("empty").hidden = list.childElementCount !== 0;
  }

  async function loadHistory(reset) {
    $("hist-status").textContent = reset ? "加载中…" : "";
    try {
      var q = new URLSearchParams({ channel: channel(), limit: "30" });
      if (token()) q.set("token", token());
      if (!reset && nextBefore != null) q.set("before", String(nextBefore));
      var res = await fetch("/api/v1/messages?" + q.toString(), {
        credentials: "same-origin",
        cache: "no-store"
      });
      var data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || "加载失败");
      appendMessages(data.messages || [], reset);
      nextBefore = data.next_before == null ? null : data.next_before;
      $("more").hidden = nextBefore == null;
      $("hist-status").textContent = "";
    } catch (err) {
      $("hist-status").textContent = err.message || "加载失败";
      if (reset) {
        $("list").replaceChildren();
        $("empty").hidden = true;
      }
    }
  }

  $("show-token").addEventListener("click", function () {
    showTokenField(true);
    $("token").focus();
  });
  $("enter").addEventListener("click", enterRoom);
  $("token").addEventListener("keydown", function (e) {
    if (e.key === "Enter") enterRoom();
  });
  $("channel").addEventListener("keydown", function (e) {
    if (e.key === "Enter") {
      e.preventDefault();
      if (info.token_required) $("token").focus();
      else enterRoom();
    }
  });
  $("send").addEventListener("click", send);
  $("pick").addEventListener("click", function () { $("file").click(); });
  $("file").addEventListener("change", function () {
    var picked = $("file").files && $("file").files[0];
    if (!picked) return;
    if (previewURL) URL.revokeObjectURL(previewURL);
    file = picked;
    previewURL = URL.createObjectURL(picked);
    $("preview").src = previewURL;
    $("preview").hidden = false;
    $("clear-photo").hidden = false;
    $("pick").textContent = "更换照片";
    setStatus("");
  });
  $("clear-photo").addEventListener("click", clearPhoto);
  $("to-history").addEventListener("click", function () {
    show("history");
    nextBefore = null;
    loadHistory(true);
  });
  $("back").addEventListener("click", function () { show("compose"); });
  $("reload").addEventListener("click", function () {
    nextBefore = null;
    loadHistory(true);
  });
  $("more").addEventListener("click", function () { loadHistory(false); });
  $("change").addEventListener("click", function () {
    $("channel").value = channel();
    $("token").value = token();
    showTokenField(!!token());
    show("setup");
  });

  loadInfo().then(function () {
    if (!channel()) {
      show("setup");
      return;
    }
    fetch("/api/v1/pair", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ channel: channel(), token: token() })
    }).then(async function (res) {
      if (res.ok) {
        show("compose");
        return;
      }
      var data = await res.json().catch(function () { return {}; });
      $("channel").value = channel();
      $("token").value = token();
      show("setup");
      $("setup-error").textContent = data.error || "请重新进入教室";
    }).catch(function () {
      show("compose");
    });
  });
})();

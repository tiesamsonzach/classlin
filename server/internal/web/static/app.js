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
  var adminSession = { configured: false, authenticated: false, allClassrooms: false, classrooms: [] };

  function applyAdmin(session) {
    adminSession = session || { configured: false, authenticated: false, allClassrooms: false, classrooms: [] };
    var authed = !!adminSession.authenticated;
    $("login").hidden = authed || !adminSession.configured;
    $("admin-bar").hidden = !authed;
    if (authed) $("admin-name").textContent = adminSession.username || "";
    var guard = $("call-guard");
    if (!adminSession.configured) {
      guard.textContent = "服务器还没有配置管理员，视频对讲已关闭。文字和照片仍可发送。";
    } else if (!authed) {
      guard.textContent = "视频对讲需要管理员登录。知道教室编号并不能发起通话。";
    } else {
      guard.textContent = "";
    }
    $("start-call").disabled = !authed;
  }

  async function refreshAdmin() {
    try {
      var res = await fetch("/api/v1/admin/session", { credentials: "same-origin", cache: "no-store" });
      if (!res.ok) throw new Error("bad");
      applyAdmin(await res.json());
    } catch (e) {
      applyAdmin({ configured: false, authenticated: false });
    }
  }

  async function loginAdmin() {
    $("login-error").textContent = "";
    var username = $("admin-user").value.trim();
    var password = $("admin-pass").value;
    if (!username || !password) {
      $("login-error").textContent = "请填写账号和密码";
      return;
    }
    $("login-btn").disabled = true;
    try {
      var res = await fetch("/api/v1/admin/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ username: username, password: password })
      });
      var data = await res.json().catch(function () { return {}; });
      if (!res.ok) throw new Error(data.error || "登录失败");
      $("admin-pass").value = "";
      await refreshAdmin();
    } catch (err) {
      $("login-error").textContent = err.message || "登录失败";
    } finally {
      $("login-btn").disabled = false;
    }
  }

  async function logoutAdmin() {
    if (callId || !call.hidden) await hangup(true);
    await fetch("/api/v1/admin/logout", { method: "POST", credentials: "same-origin" }).catch(function () {});
    await refreshAdmin();
  }

  function channel() { return localStorage.getItem("cn.channel") || ""; }
  function token() { return localStorage.getItem("cn.token") || ""; }

  var call = $("call");
  function show(name) {
    setup.hidden = name !== "setup";
    compose.hidden = name !== "compose";
    history.hidden = name !== "history";
    call.hidden = name !== "call";
    if (name === "setup") {
      $("hero-title").textContent = "发给讲台电脑";
      $("hero-sub").textContent = "输入电脑上显示的教室编号";
    } else if (name === "compose") {
      $("hero-title").textContent = "教室 " + channel();
      $("hero-sub").textContent = "写一句话，或发一张照片";
    } else if (name === "call") {
      $("hero-title").textContent = "视频对讲";
      $("hero-sub").textContent = "教室 " + channel();
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
      openSignalStream();
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
  $("login-btn").addEventListener("click", loginAdmin);
  $("admin-pass").addEventListener("keydown", function (e) {
    if (e.key === "Enter") loginAdmin();
  });
  $("logout").addEventListener("click", logoutAdmin);


  var pc = null;
  var localStream = null;
  var callId = "";
  var callBusy = false;
  var callEpoch = 0;
  var pendingIce = [];
  var pendingRemoteIce = [];
  var es = null;
  var iceServers = [{ urls: "stun:stun.l.google.com:19302" }];

  function setCallStatus(t, bad) {
    var el = $("call-status");
    el.textContent = t || "";
    el.classList.toggle("bad", !!bad);
  }

  function stopMedia() {
    if (localStream) {
      localStream.getTracks().forEach(function (t) { t.stop(); });
      localStream = null;
    }
    $("local-video").srcObject = null;
    var remote = $("remote-video");
    remote.srcObject = null;
    remote.muted = false;
  }

  function attachRemote(stream) {
    var el = $("remote-video");
    el.muted = false;
    el.volume = 1;
    el.srcObject = stream;
    if (typeof el.setSinkId === "function") {
      el.setSinkId("default").catch(function () {});
    }
    var play = el.play();
    if (play && play.catch) play.catch(function () {});
  }

  function closePeer() {
    if (pc) { try { pc.close(); } catch (e) {} pc = null; }
  }

  async function postSignal(kind, id, sdp, candidate) {
    var res = await fetch("/api/v1/signal", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({
        channel: channel(),
        token: token(),
        kind: kind,
        from: "admin",
        callId: id,
        sdp: sdp || "",
        candidate: candidate || ""
      })
    });
    var data = await res.json().catch(function () { return {}; });
    if (!res.ok) throw new Error(data.error || "信令失败");
  }

  async function hangup(send, stay) {
    callEpoch++;
    var id = callId;
    closePeer();
    stopMedia();
    pendingIce = [];
    pendingRemoteIce = [];
    callBusy = false;
    callId = "";
    if (send && id) {
      try { await postSignal("hangup", id, "", ""); } catch (e) {}
    }
    if (!stay) show("compose");
  }

  function openSignalStream() {
    if (es) { try { es.close(); } catch (e) {} es = null; }
    if (!channel()) return;
    var q = new URLSearchParams({ channel: channel() });
    if (token()) q.set("token", token());
    es = new EventSource("/api/v1/stream?" + q.toString());
    es.addEventListener("signal", function (ev) {
      try { onSignal(JSON.parse(ev.data)); } catch (e) {}
    });
    es.onerror = function () {};
  }

  async function addRemoteIce(raw) {
    var cand = JSON.parse(raw);
    if (!pc || !pc.remoteDescription) {
      pendingRemoteIce.push(cand);
      return;
    }
    await pc.addIceCandidate(cand);
  }

  async function flushRemoteIce() {
    var queued = pendingRemoteIce;
    pendingRemoteIce = [];
    for (var i = 0; i < queued.length; i++) {
      try { await pc.addIceCandidate(queued[i]); } catch (e) {}
    }
  }

  function flushLocalIce() {
    var queued = pendingIce;
    pendingIce = [];
    queued.forEach(function (payload) {
      postSignal("ice", callId, "", payload).catch(function () {});
    });
  }

  async function onSignal(sig) {
    if (!sig || !sig.kind || sig.from === "admin" || sig.from === "phone") return;
    if (sig.kind === "hangup") {
      if (!callId || sig.callId === callId) {
        hangup(false);
        setCallStatus("教室已挂断");
      }
      return;
    }
    if (sig.kind === "answer" && sig.sdp && pc && sig.callId === callId) {
      await pc.setRemoteDescription({ type: "answer", sdp: sig.sdp });
      await flushRemoteIce();
      setCallStatus("通话中，双方都能看到并听到对方");
      return;
    }
    if (sig.kind === "ice" && sig.candidate && sig.callId === callId) {
      try { await addRemoteIce(sig.candidate); } catch (e) {}
    }
  }

  async function startCall() {
    if (!adminSession || !adminSession.authenticated) {
      $("call-guard").textContent = "请先登录管理员";
      $("login").hidden = false;
      return;
    }
    if (!adminSession.allClassrooms && (adminSession.classrooms || []).indexOf(channel()) === -1) {
      $("call-guard").textContent = "这个账号不能呼叫该教室";
      return;
    }
    if (callBusy || callId) {
      setCallStatus("教室正在通话", true);
      return;
    }
    show("call");
    var epoch = callEpoch;
    callBusy = true;
    try {
      openSignalStream();
      var probe = await fetch("/api/v1/calls?channel=" + encodeURIComponent(channel()), {
        credentials: "same-origin",
        cache: "no-store"
      });
      var probeBody = await probe.json().catch(function () { return {}; });
      if (probe.status === 401) {
        callBusy = false;
        applyAdmin({ configured: true, authenticated: false });
        $("call-guard").textContent = "请先登录管理员";
        show("compose");
        return;
      }
      if (!probe.ok) {
        callBusy = false;
        $("call-guard").textContent = probeBody.error || "不能呼叫这间教室";
        show("compose");
        return;
      }
      if (probeBody.busy) {
        callBusy = false;
        $("call-guard").textContent = "教室正在通话";
        show("compose");
        return;
      }
      setCallStatus("正在打开本机摄像头和麦克风…");
      await createPeer();
      if (epoch !== callEpoch || !pc) {
        closePeer();
        stopMedia();
        callBusy = false;
        setCallStatus("已取消", true);
        return;
      }
      var offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      setCallStatus("正在呼叫教室…");
      var res = await fetch("/api/v1/calls", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ channel: channel(), sdp: offer.sdp })
      });
      var data = await res.json().catch(function () { return {}; });
      if (epoch !== callEpoch) {
        if (res.ok && data.callId) {
          callId = data.callId;
          try { await postSignal("hangup", callId, "", ""); } catch (e) {}
        }
        closePeer();
        stopMedia();
        callBusy = false;
        callId = "";
        return;
      }
      if (res.status === 409 || data.busy) {
        await hangup(false, true);
        setCallStatus(data.error || "教室正在通话", true);
        return;
      }
      if (!res.ok || !data.callId) {
        await hangup(false, true);
        setCallStatus(data.error || "不能发起通话", true);
        return;
      }
      callId = data.callId;
      flushLocalIce();
      setCallStatus("已呼叫，等待教室自动接听");
    } catch (err) {
      await hangup(false, true);
      setCallStatus(err.message || String(err), true);
    }
  }

  async function loadIce() {
    try {
      var q = new URLSearchParams();
      if (channel()) q.set("channel", channel());
      if (token()) q.set("token", token());
      var res = await fetch("/api/v1/ice?" + q.toString(), { credentials: "same-origin", cache: "no-store" });
      if (!res.ok) return;
      var data = await res.json();
      if (data.iceServers && data.iceServers.length) iceServers = data.iceServers;
    } catch (e) {}
  }

  async function createPeer() {
    closePeer();
    await loadIce();
    pc = new RTCPeerConnection({ iceServers: iceServers });
    var remoteStream = new MediaStream();
    pc.ontrack = function (ev) {
      if (ev.streams && ev.streams[0]) {
        attachRemote(ev.streams[0]);
        return;
      }
      remoteStream.addTrack(ev.track);
      attachRemote(remoteStream);
    };
    pc.onicecandidate = function (ev) {
      if (!ev.candidate) return;
      var payload = JSON.stringify(ev.candidate);
      if (!callId) {
        pendingIce.push(payload);
        return;
      }
      postSignal("ice", callId, "", payload).catch(function () {});
    };
    pc.onconnectionstatechange = function () {
      if (!pc) return;
      if (pc.connectionState === "connected") setCallStatus("通话中，双方都能看到并听到对方");
      if (pc.connectionState === "failed") setCallStatus("对讲连接失败。若在校园网外，需要配置 TURN", true);
    };
    var stream = await ensureMedia();
    stream.getTracks().forEach(function (t) { pc.addTrack(t, stream); });
    return pc;
  }

  async function ensureMedia() {
    if (localStream) return localStream;
    localStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: true });
    if (!localStream.getAudioTracks().length || !localStream.getVideoTracks().length) {
      localStream.getTracks().forEach(function (t) { t.stop(); });
      localStream = null;
      throw new Error("需要同时使用摄像头和麦克风");
    }
    var local = $("local-video");
    local.srcObject = localStream;
    local.muted = true;
    return localStream;
  }

  $("start-call").addEventListener("click", startCall);
  $("hangup").addEventListener("click", function () { hangup(true); });


  loadInfo().then(function () {
    refreshAdmin();
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
        openSignalStream();
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

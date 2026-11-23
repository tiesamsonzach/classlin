(function () {
  var $ = function (id) { return document.getElementById(id); };
  var tauri = window.__TAURI__;
  var params = new URLSearchParams(location.search);

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

  function setConn(text, kind) {
    $("conn-text").textContent = text || "";
    $("dot").className = "dot " + (kind || "off");
  }

  function showId(channel) {
    var id = (channel || "").trim() || "--------";
    $("big-id").textContent = id;
    $("channel").value = id;
    $("title").textContent = "把这个编号发给手机";
  }

  function showHome(channel) {
    $("setup").hidden = true;
    $("home").hidden = false;
    showId(channel);
  }

  function showSetup(channel) {
    $("setup").hidden = false;
    $("home").hidden = true;
    showId(channel);
  }

  function renderList(items) {
    var list = $("list");
    list.replaceChildren();
    $("empty").hidden = items.length !== 0;
    items.forEach(function (m) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "item";
      btn.dataset.id = m.id;
      btn.dataset.createdAt = m.createdAt || "";
      var time = document.createElement("time");
      time.textContent = fmt(m.createdAt);
      var p = document.createElement("p");
      p.textContent = m.body || (m.hasImage ? "老师发来一张照片" : "（空消息）");
      btn.appendChild(time);
      btn.appendChild(p);
      if (m.hasImage) {
        var badge = document.createElement("span");
        badge.className = "badge";
        badge.textContent = "含照片";
        btn.appendChild(badge);
      }
      btn.addEventListener("click", function () {
        if (tauri) tauri.core.invoke("open_message", { id: m.id });
      });
      list.appendChild(btn);
    });
  }

  function upsert(m) {
    if (!m || !m.id) return;
    var items = [];
    $("list").querySelectorAll(".item").forEach(function (node) {
      items.push(node.dataset.id);
    });
    if (items.indexOf(m.id) !== -1) return;
    var current = [];
    $("list").querySelectorAll(".item").forEach(function (node) {
      current.push({
        id: node.dataset.id,
        createdAt: node.dataset.createdAt,
        body: node.querySelector("p") ? node.querySelector("p").textContent : "",
        hasImage: !!node.querySelector(".badge")
      });
    });
    current.unshift(m);
    renderList(current.slice(0, 50));
  }

  async function refresh() {
    if (!tauri) return;
    $("list-status").textContent = "加载中…";
    try {
      var items = await tauri.core.invoke("list_history");
      renderList(items || []);
      $("list-status").textContent = "";
    } catch (err) {
      $("list-status").textContent = String(err);
    }
  }

  function fill(cfg) {
    $("s-url").value = cfg.serverUrl || "";
    $("url").value = cfg.serverUrl || "";
    $("s-token").value = cfg.token || "";
    $("token").value = cfg.token || "";
    showId(cfg.channel || "");
  }

  async function save(fromSettings) {
    var errId = fromSettings ? "save-error" : "setup-error";
    var input = {
      serverUrl: fromSettings ? $("url").value : $("s-url").value,
      channel: fromSettings ? $("channel").value : $("big-id").textContent,
      token: fromSettings ? $("token").value : $("s-token").value
    };
    $(errId).textContent = "";
    if (!tauri) {
      $(errId).textContent = "请在教室电脑的程序里保存";
      return;
    }
    try {
      await tauri.core.invoke("save_config", { input: input });
      showHome(input.channel.trim());
      refresh();
    } catch (err) {
      $(errId).textContent = String(err);
    }
  }

  async function copyId() {
    var text = $("big-id").textContent.trim();
    $("copy-note").textContent = "手机打开同一网址，填这一串就能发。";
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        throw new Error("no clipboard");
      }
      $("copy-id").textContent = "已复制";
      setTimeout(function () { $("copy-id").textContent = "复制编号"; }, 1600);
    } catch (err) {
      var range = document.createRange();
      range.selectNodeContents($("big-id"));
      var sel = window.getSelection();
      sel.removeAllRanges();
      sel.addRange(range);
      $("copy-note").textContent = "已选中编号，按 Ctrl+C 复制";
    }
  }


  var composeFile = null;
  var composePreviewURL = "";
  var pc = null;
  var localStream = null;
  var callId = "";
  var answerKey = "";
  var callBusy = false;
  var callEpoch = 0;
  var pendingRemoteIce = [];
  var iceServers = [{ urls: "stun:stun.l.google.com:19302" }];

  function setComposeStatus(text, bad) {
    $("compose-status").textContent = text || "";
    $("compose-status").className = bad ? "bad" : "hint";
  }

  function clearComposePhoto() {
    composeFile = null;
    if (composePreviewURL) URL.revokeObjectURL(composePreviewURL);
    composePreviewURL = "";
    $("compose-file").value = "";
    $("compose-preview").hidden = true;
    $("compose-preview").removeAttribute("src");
    $("compose-clear").hidden = true;
    $("compose-pick").textContent = "添加照片";
  }

  function fileToBase64(file) {
    return new Promise(function (resolve, reject) {
      var reader = new FileReader();
      reader.onload = function () { resolve(String(reader.result || "")); };
      reader.onerror = function () { reject(new Error("读取照片失败")); };
      reader.readAsDataURL(file);
    });
  }

  async function composeSend() {
    var body = $("compose-text").value.trim();
    if (!body && !composeFile) {
      setComposeStatus("先写一句话，或添加一张照片", true);
      return;
    }
    if (!tauri) {
      setComposeStatus("请在教室电脑程序里发送", true);
      return;
    }
    $("compose-send").disabled = true;
    setComposeStatus("发送中…", false);
    try {
      if (composeFile) {
        var dataUrl = await fileToBase64(composeFile);
        await tauri.core.invoke("send_image", {
          input: {
            body: body,
            dataBase64: dataUrl,
            fileName: composeFile.name || "photo.jpg",
            mime: composeFile.type || "image/jpeg"
          }
        });
      } else {
        await tauri.core.invoke("send_text", { input: { body: body } });
      }
      $("compose-text").value = "";
      clearComposePhoto();
      setComposeStatus("已发送", false);
      refresh();
    } catch (err) {
      setComposeStatus(String(err), true);
    } finally {
      $("compose-send").disabled = false;
    }
  }

  function setCallStatus(text) {
    $("call-status").textContent = text || "";
  }

  function stopLocalMedia() {
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

  function showCallPanel() {
    $("call-panel").hidden = false;
    $("call-toggle").textContent = "收起面板";
  }

  function closePeer() {
    if (pc) {
      try { pc.close(); } catch (e) {}
      pc = null;
    }
  }

  function sendSignal(kind, id, sdp, candidate) {
    if (!tauri || !id) return Promise.resolve();
    return tauri.core.invoke("send_signal", {
      input: {
        kind: kind,
        callId: id,
        sdp: sdp || null,
        candidate: candidate || null,
        answerKey: answerKey || null
      }
    });
  }

  async function hangup(send) {
    callEpoch++;
    var id = callId;
    var key = answerKey;
    closePeer();
    stopLocalMedia();
    pendingRemoteIce = [];
    callBusy = false;
    callId = "";
    answerKey = "";
    if (send && id && key) {
      answerKey = key;
      try { await sendSignal("hangup", id, null, null); } catch (e) {}
      answerKey = "";
    }
  }

  async function onSignal(sig) {
    if (!sig || !sig.kind || sig.from === "desktop") return;
    if (sig.kind === "offer-rejected") {
      if (!callBusy && !callId) setCallStatus(sig.error || "未授权来电，不接听，摄像头未打开");
      return;
    }
    if (sig.kind === "hangup") {
      if (!callId || sig.callId === callId) {
        hangup(false);
        setCallStatus("管理员已挂断");
      }
      return;
    }
    if (sig.kind === "offer") {
      var busyDecision = decideIncomingCall({ inCall: callBusy || !!callId, verified: true });
      if (busyDecision.action === "busy") {
        setCallStatus("正在通话，已拒绝新的来电");
        return;
      }
      var epoch = callEpoch;
      callBusy = true;
      showCallPanel();
      setCallStatus("正在确认来电身份…");
      try {
        var verdict = await tauri.core.invoke("verify_call", { callId: sig.callId, claim: true });
        if (epoch !== callEpoch) {
          if (verdict && verdict.answerKey) {
            answerKey = verdict.answerKey;
            callId = sig.callId;
            try { await sendSignal("hangup", callId, null, null); } catch (e) {}
            answerKey = "";
            callId = "";
          }
          return;
        }
        var decision = decideIncomingCall({
          inCall: false,
          verified: !!(verdict && verdict.ok === true && verdict.answerKey)
        });
        if (!decision.openCamera) {
          callBusy = false;
          setCallStatus("未授权来电，不接听，摄像头未打开");
          return;
        }
        if (!sig.sdp) {
          answerKey = verdict.answerKey;
          callId = sig.callId;
          setCallStatus("来电内容不完整，摄像头未打开");
          hangup(true);
          return;
        }
        answerKey = verdict.answerKey;
        callId = sig.callId;
        if (tauri) {
          try { await tauri.core.invoke("focus_for_call"); } catch (e) {}
        }
        setCallStatus("正在接通" + (verdict.caller ? "管理员 " + verdict.caller : "") + "…");
        await createPeer();
        if (epoch !== callEpoch || !pc) {
          closePeer();
          stopLocalMedia();
          if (answerKey && callId) {
            try { await sendSignal("hangup", callId, null, null); } catch (e) {}
          }
          answerKey = "";
          callId = "";
          callBusy = false;
          return;
        }
        await pc.setRemoteDescription({ type: "offer", sdp: sig.sdp });
        await flushRemoteIce();
        var answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        await sendSignal("answer", callId, answer.sdp, null);
        setCallStatus("通话中。本机使用摄像头、麦克风和扬声器");
      } catch (err) {
        setCallStatus(String(err && err.message || err));
        hangup(true);
      }
      return;
    }
    if (sig.kind === "ice" && sig.candidate && sig.callId === callId) {
      try { await addRemoteIce(sig.candidate); } catch (e) {}
    }
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

  async function loadIce() {
    if (!tauri) return;
    try {
      var cfg = await tauri.core.invoke("ice_servers");
      if (cfg && cfg.iceServers && cfg.iceServers.length) iceServers = cfg.iceServers;
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
      if (!ev.candidate || !callId) return;
      sendSignal("ice", callId, null, JSON.stringify(ev.candidate)).catch(function () {});
    };
    pc.onconnectionstatechange = function () {
      if (!pc) return;
      if (pc.connectionState === "connected") setCallStatus("通话中。本机使用摄像头、麦克风和扬声器");
      if (pc.connectionState === "failed") setCallStatus("对讲连接失败。若在校园网外，需要配置 TURN");
    };
    var stream = await ensureLocalMedia();
    stream.getTracks().forEach(function (track) { pc.addTrack(track, stream); });
    return pc;
  }

  async function ensureLocalMedia() {
    if (localStream) return localStream;
    localStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: true });
    if (!localStream.getAudioTracks().length || !localStream.getVideoTracks().length) {
      localStream.getTracks().forEach(function (t) { t.stop(); });
      localStream = null;
      throw new Error("需要同时使用本机摄像头和麦克风");
    }
    var local = $("local-video");
    local.srcObject = localStream;
    local.muted = true;
    return localStream;
  }

  $("compose-pick").addEventListener("click", function () { $("compose-file").click(); });
  $("compose-clear").addEventListener("click", clearComposePhoto);
  $("compose-file").addEventListener("change", function () {
    var picked = $("compose-file").files && $("compose-file").files[0];
    if (!picked) return;
    if (composePreviewURL) URL.revokeObjectURL(composePreviewURL);
    composeFile = picked;
    composePreviewURL = URL.createObjectURL(picked);
    $("compose-preview").src = composePreviewURL;
    $("compose-preview").hidden = false;
    $("compose-clear").hidden = false;
    $("compose-pick").textContent = "更换照片";
    setComposeStatus("", false);
  });
  $("compose-send").addEventListener("click", composeSend);
  $("call-toggle").addEventListener("click", function () {
    $("call-panel").hidden = !$("call-panel").hidden;
    $("call-toggle").textContent = $("call-panel").hidden ? "打开面板" : "收起面板";
  });
  $("call-hangup").addEventListener("click", function () {
    hangup(true).then(function () { setCallStatus("已挂断"); });
  });


  $("save-setup").addEventListener("click", function () { save(false); });
  $("save").addEventListener("click", function () { save(true); });
  $("refresh").addEventListener("click", refresh);
  $("copy-id").addEventListener("click", copyId);
  $("regen").addEventListener("click", function () {
    if (!tauri) {
      $("channel").value = "7K3M-P9WQ";
      showId("7K3M-P9WQ");
      return;
    }
    tauri.core.invoke("generate_classroom_id").then(function (id) {
      $("channel").value = id;
      showId(id);
    });
  });
  $("channel").addEventListener("input", function () {
    showId($("channel").value);
  });

  if (!tauri) {
    if (params.get("sw") && window.__applyUiScale) window.__applyUiScale(Number(params.get("sw")));
    fill({ serverUrl: "https://notify.example.com", channel: "7K3M-P9WQ", token: "" });
    if (params.get("preview") === "home") {
      showHome("7K3M-P9WQ");
      setConn("已连接", "on");
      renderList([
        { id: "1", createdAt: new Date().toISOString(), body: "请翻开课本第 12 页，完成练习二。", hasImage: false },
        { id: "2", createdAt: new Date(Date.now() - 3600000).toISOString(), body: "黑板这道题", hasImage: true }
      ]);
    } else {
      showSetup("7K3M-P9WQ");
    }
    return;
  }

  tauri.event.listen("metrics", function (event) {
    if (window.__applyUiScale) window.__applyUiScale(event.payload.logicalWidth);
  });
  tauri.event.listen("status", function (event) {
    setConn(event.payload.text, event.payload.kind);
  });
  tauri.event.listen("message", function (event) {
    upsert(event.payload);
  });
  tauri.event.listen("signal", function (event) {
    onSignal(event.payload || {});
  });
  tauri.core.invoke("screen_metrics").then(function (m) {
    if (window.__applyUiScale) window.__applyUiScale(m.logicalWidth);
  });
  tauri.core.invoke("connection_status").then(function (s) { setConn(s.text, s.kind); });
  tauri.core.invoke("load_config").then(function (cfg) {
    fill(cfg || {});
    if (cfg && cfg.serverUrl && cfg.channel) {
      showHome(cfg.channel);
      refresh();
    } else {
      showSetup((cfg && cfg.channel) || "");
    }
  });
})();

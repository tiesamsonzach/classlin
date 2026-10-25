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

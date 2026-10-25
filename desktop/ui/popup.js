(function () {
  var $ = function (id) { return document.getElementById(id); };
  var tauri = window.__TAURI__;

  function fmt(iso) {
    var d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    var now = new Date();
    var hh = String(d.getHours()).padStart(2, "0");
    var mm = String(d.getMinutes()).padStart(2, "0");
    var same = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
    var clock = same ? ("今天 " + hh + ":" + mm) : ((d.getMonth() + 1) + "月" + d.getDate() + "日 " + hh + ":" + mm);
    return clock;
  }

  function bodyClass(text) {
    var n = Array.from(text || "").length;
    if (n > 180) return "body long";
    if (n > 60) return "body mid";
    return "body";
  }

  function fileSrc(path) {
    if (!path) return "";
    try {
      if (tauri && tauri.core && tauri.core.convertFileSrc) return tauri.core.convertFileSrc(path);
    } catch (e) {}
    return "";
  }

  function render(msg) {
    if (!msg) return;
    $("kicker").textContent = "课堂通知 · " + fmt(msg.createdAt);
    $("title").textContent = msg.channel ? ("教室 " + msg.channel) : "课堂通知";
    var text = (msg.body || "").trim();
    if (!text) text = msg.hasImage ? "老师发来一张照片" : "新消息";
    var body = $("body");
    body.className = bodyClass(text);
    body.textContent = text;
    var photo = $("photo");
    var img = $("img");
    var note = $("photo-note");
    img.hidden = true;
    note.hidden = true;
    img.removeAttribute("src");
    if (msg.imageLoading) {
      photo.hidden = false;
      note.hidden = false;
      note.textContent = "照片加载中…";
      return;
    }
    if (msg.imageError) {
      photo.hidden = false;
      note.hidden = false;
      note.textContent = "照片加载失败";
      return;
    }
    var src = msg.previewSrc || fileSrc(msg.imagePath);
    if (src) {
      photo.hidden = false;
      img.hidden = false;
      img.src = src;
      return;
    }
    photo.hidden = !msg.hasImage;
  }

  $("ok").addEventListener("click", function () {
    if (tauri) tauri.core.invoke("dismiss_popup");
  });
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && tauri) tauri.core.invoke("dismiss_popup");
  });
  $("img").addEventListener("error", function () {
    $("img").hidden = true;
    $("photo-note").hidden = false;
    $("photo-note").textContent = "照片加载失败";
    $("photo").hidden = false;
  });

  if (!tauri) {
    var params = new URLSearchParams(location.search);
    if (params.get("sw") && window.__applyUiScale) window.__applyUiScale(Number(params.get("sw")));
    var photo = params.get("photo") === "1";
    render({
      channel: "301",
      body: params.get("body") || "请翻开课本第 12 页，完成练习二。",
      createdAt: new Date().toISOString(),
      hasImage: photo,
      imagePath: null,
      imageLoading: false,
      imageError: false,
      previewSrc: photo ? "data:image/svg+xml," + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="800" height="500"><rect width="800" height="500" fill="#1d3b32"/><text x="40" y="240" fill="#f4efe4" font-size="72" font-family="sans-serif">黑板照片</text></svg>') : ""
    });
    return;
  }

  tauri.event.listen("metrics", function (event) {
    if (window.__applyUiScale) window.__applyUiScale(event.payload.logicalWidth);
  });
  tauri.event.listen("message", function (event) { render(event.payload); });
  tauri.core.invoke("screen_metrics").then(function (m) {
    if (window.__applyUiScale) window.__applyUiScale(m.logicalWidth);
  });
  tauri.core.invoke("latest_message").then(function (msg) { if (msg) render(msg); });
})();

(function () {
  function apply(logicalWidth) {
    var w = Number(logicalWidth) || (window.screen && window.screen.width) || 1920;
    if (w < 100) w = 1920;
    var ui = Math.max(1, Math.min(2.4, w / 1920));
    document.documentElement.style.setProperty("--ui", String(ui));
    document.documentElement.style.fontSize = (18 * ui) + "px";
  }
  apply();
  window.__applyUiScale = apply;
})();

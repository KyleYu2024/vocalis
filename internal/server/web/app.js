document.addEventListener("click", function (e) {
  var el = e.target.closest("[data-copy]");
  if (!el) return;
  var text = el.getAttribute("data-copy");
  var done = function () {
    var old = el.textContent;
    el.textContent = "已复制";
    setTimeout(function () { el.textContent = old; }, 1500);
  };
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(done, function () { window.prompt("复制订阅地址", text); });
  } else {
    window.prompt("复制订阅地址", text);
  }
});

document.addEventListener("submit", function (e) {
  var form = e.target;
  if (!form.matches("[data-confirm]")) return;
  if (!window.confirm(form.getAttribute("data-confirm"))) e.preventDefault();
});

// 输入即过滤：书库页按书过滤，详情页按集过滤。
function wireFilter(boxId, itemSel, infoId, unit) {
  var box = document.getElementById(boxId);
  if (!box) return;
  var items = Array.prototype.slice.call(document.querySelectorAll(itemSel));
  var info = document.getElementById(infoId);
  var empty = document.getElementById("qempty");
  function apply() {
    var v = box.value.trim().toLowerCase();
    var n = 0;
    items.forEach(function (el) {
      var hay = (el.getAttribute("data-search") || "").toLowerCase();
      var hit = !v || hay.indexOf(v) >= 0;
      el.style.display = hit ? "" : "none";
      if (hit) n++;
    });
    if (info) info.textContent = v ? n + " / " + items.length + " " + unit : "";
    if (empty) empty.style.display = v && n === 0 ? "" : "none";
  }
  box.addEventListener("input", apply);
  apply();
}

wireFilter("q", "#bookgrid [data-search]", "qinfo", "本");
wireFilter("chq", "tbody [data-search]", "chinfo", "集");

// 按 "/" 直接跳到搜索框
document.addEventListener("keydown", function (e) {
  if (e.key !== "/" || e.metaKey || e.ctrlKey) return;
  var box = document.getElementById("q") || document.getElementById("chq");
  if (!box || document.activeElement === box) return;
  var tag = (document.activeElement.tagName || "").toLowerCase();
  if (tag === "input" || tag === "textarea") return;
  e.preventDefault();
  box.focus();
});

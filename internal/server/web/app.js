// 复制到剪贴板。navigator.clipboard 只在 HTTPS 或 localhost 下可用，
// 局域网 http://NAS-IP:8080 属于不安全上下文，必须用 execCommand 兜底，
// 否则按钮只会弹一个对话框，什么都没复制。
function legacyCopy(text) {
  var ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "0";
  ta.style.left = "0";
  ta.style.width = "1px";
  ta.style.height = "1px";
  ta.style.padding = "0";
  ta.style.border = "none";
  ta.style.outline = "none";
  ta.style.boxShadow = "none";
  ta.style.background = "transparent";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  var sel = document.getSelection();
  var prev = sel && sel.rangeCount > 0 ? sel.getRangeAt(0) : null;
  ta.focus();
  ta.select();
  // iOS Safari 需要显式设置选区范围。
  try { ta.setSelectionRange(0, text.length); } catch (err) { /* 忽略 */ }
  var ok = false;
  try { ok = document.execCommand("copy"); } catch (err) { ok = false; }
  document.body.removeChild(ta);
  if (prev && sel) {
    sel.removeAllRanges();
    sel.addRange(prev);
  }
  return ok;
}

// 同步、必定可用的复制路径放在用户手势里执行，异步 API 失败时也会落到这里。
function copyText(text, done) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(
      function () { done(true); },
      function () { done(legacyCopy(text)); }
    );
    return;
  }
  done(legacyCopy(text));
}

document.addEventListener("click", function (e) {
  var el = e.target.closest("[data-copy]");
  if (!el) return;
  var text = el.getAttribute("data-copy");
  if (!text) return;
  // 按钮旁边通常有一个只读输入框，顺手选中方便用户手动复制。
  var field = el.parentNode ? el.parentNode.querySelector("input.url") : null;
  copyText(text, function (ok) {
    if (ok && field) field.select();
    var label = el.getAttribute("data-copy-label");
    if (label === null) {
      label = el.textContent;
      el.setAttribute("data-copy-label", label);
    }
    el.textContent = ok ? "已复制" : "复制失败";
    clearTimeout(el.copyTimer);
    el.copyTimer = setTimeout(function () {
      el.textContent = label;
    }, 1500);
    if (!ok) window.prompt("请手动复制下面的地址", text);
  });
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

/* Apply the saved theme before first paint. Shared by the app and docs pages. */
(function () {
  var theme = "dark";
  try {
    var saved = localStorage.getItem("rmslist-theme");
    if (saved === "light" || saved === "dark") theme = saved;
  } catch (_) {}
  document.documentElement.dataset.theme = theme;
})();

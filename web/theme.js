/* Apply the saved theme and mobile view before first paint.
 * Shared by the app and docs pages. */
(function () {
  var theme = "dark";
  var view = "map";
  try {
    var savedTheme = localStorage.getItem("rmslist-theme");
    if (savedTheme === "light" || savedTheme === "dark") theme = savedTheme;
    if (localStorage.getItem("rmslist-mobile-view") === "list") view = "list";
  } catch (_) {}
  var root = document.documentElement;
  root.dataset.theme = theme;
  root.dataset.mobileView = view;
})();

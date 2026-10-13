// The skin, before anything paints. A visitor's choice is kept where the docs
// keep theirs (Hextra's "color-theme"), so it follows them between pages;
// with none stored, the stylesheet follows the system.
(function () {
  var theme = null;
  try {
    theme = localStorage.getItem("color-theme");
  } catch (e) {}
  if (theme === "light" || theme === "dark") {
    document.documentElement.classList.add(theme);
  }
})();

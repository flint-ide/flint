// Applies the saved light/dark choice before the page paints, and wires the toggle.
(function () {
  var saved = null
  try { saved = localStorage.getItem('flint-docs-theme') } catch (e) {}
  if (saved === 'light' || saved === 'dark') document.documentElement.dataset.theme = saved
  document.addEventListener('DOMContentLoaded', function () {
    var btn = document.querySelector('.theme-toggle')
    if (btn) btn.addEventListener('click', function () {
      var dark = document.documentElement.dataset.theme
        ? document.documentElement.dataset.theme === 'dark'
        : window.matchMedia('(prefers-color-scheme: dark)').matches
      var next = dark ? 'light' : 'dark'
      document.documentElement.dataset.theme = next
      try { localStorage.setItem('flint-docs-theme', next) } catch (e) {}
    })
    var menu = document.querySelector('.menu')
    if (menu) menu.addEventListener('click', function () {
      var open = document.body.classList.toggle('nav-open')
      menu.setAttribute('aria-expanded', String(open))
    })
  })
})()

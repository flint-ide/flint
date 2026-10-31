// Client-side search over search.json: page titles, headings and text.
// No dependencies; the index loads on first focus.
(function () {
  var root = document.currentScript.dataset.root || ''
  var input, list, index = null, active = -1

  function load() {
    if (index) return Promise.resolve(index)
    return fetch(root + 'search.json').then(function (r) { return r.json() }).then(function (d) { index = d; return d })
  }

  function score(entry, terms) {
    var total = 0
    var title = entry.title.toLowerCase(), text = entry.text.toLowerCase()
    for (var i = 0; i < terms.length; i++) {
      var t = terms[i], s = 0
      if (title.indexOf(t) >= 0) s += 10
      for (var h = 0; h < (entry.headings || []).length; h++) {
        if (entry.headings[h].text.toLowerCase().indexOf(t) >= 0) { s += 4; break }
      }
      if (text.indexOf(t) >= 0) s += 1
      if (s === 0) return 0 // every word must match somewhere
      total += s
    }
    return total
  }

  function bestHeading(entry, terms) {
    var hs = entry.headings || []
    for (var h = 0; h < hs.length; h++) {
      var ht = hs[h].text.toLowerCase()
      if (terms.every(function (t) { return ht.indexOf(t) >= 0 })) return hs[h]
    }
    return null
  }

  function snippet(entry, terms) {
    var text = entry.text, lower = text.toLowerCase(), at = lower.indexOf(terms[0])
    if (at < 0) return text.slice(0, 120)
    var start = Math.max(0, at - 40)
    return (start > 0 ? '…' : '') + text.slice(start, start + 140) + '…'
  }

  function render(q) {
    var terms = q.toLowerCase().split(/\s+/).filter(Boolean)
    list.textContent = ''
    active = -1
    if (!terms.length) { list.hidden = true; return }
    var hits = index.map(function (e) { return { e: e, s: score(e, terms) } })
      .filter(function (x) { return x.s > 0 })
      .sort(function (a, b) { return b.s - a.s })
      .slice(0, 8)
    if (!hits.length) {
      var none = document.createElement('li')
      none.className = 'empty'
      none.textContent = 'No results'
      list.appendChild(none)
    }
    hits.forEach(function (hit) {
      var h = bestHeading(hit.e, terms)
      var li = document.createElement('li')
      var a = document.createElement('a')
      a.href = root + hit.e.url + (h ? '#' + h.id : '')
      var title = document.createElement('strong')
      title.textContent = hit.e.title + (h ? ' › ' + h.text : '')
      var meta = document.createElement('span')
      meta.className = 'meta'
      meta.textContent = hit.e.section
      var text = document.createElement('span')
      text.className = 'snippet'
      text.textContent = snippet(hit.e, terms)
      a.append(title, meta, text)
      li.appendChild(a)
      list.appendChild(li)
    })
    list.hidden = false
  }

  function move(delta) {
    var items = list.querySelectorAll('a')
    if (!items.length) return
    active = (active + delta + items.length) % items.length
    items.forEach(function (el, i) { el.classList.toggle('selected', i === active) })
  }

  document.addEventListener('DOMContentLoaded', function () {
    input = document.getElementById('search')
    list = document.getElementById('results')
    if (!input) return
    input.addEventListener('focus', load)
    input.addEventListener('input', function () { load().then(function () { render(input.value) }) })
    input.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowDown') { e.preventDefault(); move(1) }
      else if (e.key === 'ArrowUp') { e.preventDefault(); move(-1) }
      else if (e.key === 'Enter') {
        var sel = list.querySelector('a.selected') || list.querySelector('a')
        if (sel) location.href = sel.href
      } else if (e.key === 'Escape') { input.value = ''; list.hidden = true; input.blur() }
    })
    document.addEventListener('keydown', function (e) {
      if (e.key === '/' && document.activeElement !== input) { e.preventDefault(); input.focus() }
    })
    document.addEventListener('click', function (e) {
      if (!e.target.closest('.search')) list.hidden = true
    })
  })
})()

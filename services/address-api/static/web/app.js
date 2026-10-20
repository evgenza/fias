/* Демо-клиент address-api: подсказки с debounce, карточка объекта, дети. */
"use strict";

(function () {
  var DEBOUNCE_MS = 250;

  var KIND_LABELS = {
    addr_obj: "адресный объект",
    house: "дом",
    apartment: "квартира",
    room: "комната",
    stead: "участок",
    carplace: "машино-место"
  };

  var input = document.getElementById("search-input");
  var list = document.getElementById("suggest-list");
  var status = document.getElementById("status");
  var details = document.getElementById("details");
  var detailsAddress = document.getElementById("details-address");
  var detailsFields = document.getElementById("details-fields");
  var cacheBadge = document.getElementById("cache-badge");
  var parentWrap = document.getElementById("parent-link-wrap");
  var parentLink = document.getElementById("parent-link");
  var childrenTitle = document.getElementById("children-title");
  var childrenList = document.getElementById("children-list");

  var debounceTimer = null;
  var detailsSeq = 0;
  var requestSeq = 0; // защита от гонки: показываем только последний ответ

  function kindLabel(kind) {
    return KIND_LABELS[kind] || kind;
  }

  function setStatus(text, isError) {
    status.textContent = text || "";
    status.classList.toggle("error", Boolean(isError));
  }

  function hideSuggest() {
    list.hidden = true;
    list.innerHTML = "";
  }

  function fetchJSON(url) {
    return fetch(url).then(function (resp) {
      if (!resp.ok) {
        return resp.json().catch(function () { return null; }).then(function (body) {
          var message = (body && body.error && body.error.message) || ("HTTP " + resp.status);
          throw new Error(message);
        });
      }
      return resp;
    });
  }

  function suggest(query) {
    var seq = ++requestSeq;
    fetchJSON("/api/v1/suggest?q=" + encodeURIComponent(query) + "&limit=10")
      .then(function (resp) { return resp.json(); })
      .then(function (data) {
        if (seq !== requestSeq) { return; } // пришёл более новый ответ
        renderSuggest(data.items || []);
        setStatus(data.items && data.items.length ? "" : "Ничего не найдено");
      })
      .catch(function (err) {
        if (seq !== requestSeq) { return; }
        hideSuggest();
        setStatus("Ошибка подсказок: " + err.message, true);
      });
  }

  function renderSuggest(items) {
    list.innerHTML = "";
    if (!items.length) {
      hideSuggest();
      return;
    }
    items.forEach(function (item) {
      var li = document.createElement("li");
      var address = document.createElement("span");
      address.textContent = item.full_address;
      var kind = document.createElement("span");
      kind.className = "kind";
      kind.textContent = kindLabel(item.kind);
      li.appendChild(address);
      li.appendChild(kind);
      li.addEventListener("click", function () {
        input.value = item.full_address;
        hideSuggest();
        showDetails(item.object_id);
      });
      list.appendChild(li);
    });
    list.hidden = false;
  }

  function flagBadge(value, label) {
    var span = document.createElement("span");
    span.className = "badge " + (value ? "badge-on" : "badge-off");
    span.textContent = label + ": " + (value ? "да" : "нет");
    return span;
  }

  function field(term, valueNode) {
    var dt = document.createElement("dt");
    dt.textContent = term;
    var dd = document.createElement("dd");
    if (typeof valueNode === "string") {
      dd.textContent = valueNode;
    } else {
      dd.appendChild(valueNode);
    }
    detailsFields.appendChild(dt);
    detailsFields.appendChild(dd);
  }

  function flagsNode(item) {
    var wrap = document.createElement("span");
    wrap.appendChild(flagBadge(item.is_actual, "актуален"));
    wrap.appendChild(document.createTextNode(" "));
    wrap.appendChild(flagBadge(item.is_active, "активен"));
    return wrap;
  }

  function showDetails(objectID) {
    var seq = ++detailsSeq;
    ++requestSeq;
    clearTimeout(debounceTimer);
    childrenTitle.hidden = true;
    childrenList.innerHTML = "";
    setStatus("Загрузка карточки…");
    fetchJSON("/api/v1/addresses/" + encodeURIComponent(objectID))
      .then(function (resp) {
        var fromCache = resp.headers.get("X-Cache") === "hit";
        return resp.json().then(function (data) {
          if (seq !== detailsSeq) { return; }
          renderDetails(data, fromCache);
          loadChildren(objectID, seq);
          setStatus("");
        });
      })
      .catch(function (err) {
        if (seq !== detailsSeq) { return; }
        details.hidden = true;
        setStatus("Ошибка карточки: " + err.message, true);
      });
  }

  function renderDetails(item, fromCache) {
    details.hidden = false;
    detailsAddress.textContent = item.full_address || "(без адресной строки)";
    cacheBadge.hidden = !fromCache;

    detailsFields.innerHTML = "";
    field("object_id", String(item.object_id));
    field("object_guid", item.object_guid);
    field("вид", kindLabel(item.kind) + " (" + item.kind + ")");
    field("наименование", (item.type_name ? item.type_name + " " : "") + item.name);
    field("уровень", String(item.level));
    field("регион", item.region || "—");
    field("флаги", flagsNode(item));
    field("дочерних объектов", String(item.children_count));

    if (item.parent_object_id != null) {
      parentWrap.hidden = false;
      parentLink.textContent = "объект " + item.parent_object_id;
      parentLink.onclick = function (ev) {
        ev.preventDefault();
        showDetails(item.parent_object_id);
      };
    } else {
      parentWrap.hidden = true;
    }

    childrenTitle.hidden = true;
    childrenList.innerHTML = "";
  }

  function loadChildren(objectID, seq) {
    fetchJSON("/api/v1/addresses/" + encodeURIComponent(objectID) + "/children?limit=50")
      .then(function (resp) { return resp.json(); })
      .then(function (data) {
        if (seq !== detailsSeq) { return; }
        var items = data.items || [];
        if (!items.length) { return; }
        childrenTitle.hidden = false;
        childrenList.innerHTML = "";
        items.forEach(function (child) {
          var li = document.createElement("li");
          var a = document.createElement("a");
          a.href = "#";
          a.textContent = (child.type_name ? child.type_name + " " : "") + child.name;
          a.addEventListener("click", function (ev) {
            ev.preventDefault();
            showDetails(child.object_id);
          });
          var kind = document.createElement("span");
          kind.className = "kind";
          kind.textContent = kindLabel(child.kind);
          li.appendChild(a);
          li.appendChild(kind);
          childrenList.appendChild(li);
        });
      })
      .catch(function () {
        /* дети — необязательная часть карточки, ошибку не показываем */
      });
  }

  input.addEventListener("input", function () {
    ++requestSeq;
    var query = input.value.trim();
    clearTimeout(debounceTimer);
    if (query.length < 2) {
      hideSuggest();
      setStatus(query.length ? "Введите хотя бы 2 символа" : "");
      return;
    }
    setStatus("Поиск…");
    debounceTimer = setTimeout(function () { suggest(query); }, DEBOUNCE_MS);
  });

  document.addEventListener("click", function (ev) {
    if (!list.hidden && !list.contains(ev.target) && ev.target !== input) {
      hideSuggest();
    }
  });

  input.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape") { hideSuggest(); }
  });
})();

// BEGIN SKUGGSJA USAGE CHART
(function () {
  "use strict";

  const names = { claude: "Claude", codex: "Codex", cursor: "Cursor", hermes: "Hermes" };
  const formatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });

  // A deterministic, tangent-circle layout. Radius is sqrt(count): ink area,
  // never radius or diameter, represents the recorded count. Gaps add no data.
  function pack(values) {
    if (!values.length) return { circles: [], x: 0, y: 0, width: 1, height: 1 };
    const gap = Math.sqrt(Math.max.apply(null, values)) * 0.06;
    const circles = [];
    const bounds = function (items) {
      const left = Math.min.apply(null, items.map(function (c) { return c.x - c.r; }));
      const top = Math.min.apply(null, items.map(function (c) { return c.y - c.r; }));
      return {
        x: left, y: top,
        width: Math.max.apply(null, items.map(function (c) { return c.x + c.r; })) - left,
        height: Math.max.apply(null, items.map(function (c) { return c.y + c.r; })) - top
      };
    };
    values.forEach(function (value, index) {
      const radius = Math.sqrt(value);
      if (index === 0) {
        circles.push({ x: 0, y: 0, r: radius });
        return;
      }
      const previous = bounds(circles);
      const candidates = [{ x: previous.x + previous.width + radius + gap, y: 0, r: radius }];
      circles.forEach(function (a, i) {
        circles.slice(i + 1).forEach(function (b) {
          const dx = b.x - a.x, dy = b.y - a.y;
          const distance = Math.hypot(dx, dy);
          const ar = a.r + radius + gap, br = b.r + radius + gap;
          if (!distance || distance > ar + br || distance < Math.abs(ar - br)) return;
          const along = (ar * ar - br * br + distance * distance) / (2 * distance);
          const across = Math.sqrt(Math.max(0, ar * ar - along * along));
          [-1, 1].forEach(function (side) {
            candidates.push({
              x: a.x + along * dx / distance + side * across * dy / distance,
              y: a.y + along * dy / distance - side * across * dx / distance,
              r: radius
            });
          });
        });
      });
      let best = candidates[0], score = Infinity;
      candidates.forEach(function (candidate) {
        const overlaps = circles.some(function (other) {
          return Math.hypot(candidate.x - other.x, candidate.y - other.y) < radius + other.r + gap - 1e-8;
        });
        if (overlaps) return;
        const box = bounds(circles.concat(candidate));
        const cost = box.width * box.width + box.height * box.height;
        if (cost < score) { best = candidate; score = cost; }
      });
      circles.push(best);
    });
    const box = bounds(circles), padding = gap * 2;
    return {
      circles: circles, x: box.x - padding, y: box.y - padding,
      width: box.width + padding * 2, height: box.height + padding * 2
    };
  }

  function text(value, fallback, limit) {
    const clean = value === undefined || value === null ? "" : String(value)
      .replace(/[\u0000-\u001f\u007f]+/g, " ")
      .replace(/\/(?:Users|home)\/[^\s,;]+/g, "[local path]")
      .replace(/[A-Za-z]:\\[^\s,;]+/g, "[local path]")
      .replace(/\s+/g, " ").trim();
    if (!clean) return fallback;
    return clean.length > limit ? clean.slice(0, limit - 1).trimEnd() + "…" : clean;
  }

  function count(value) {
    return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : null;
  }

  function countLabel(value, metric) {
    if (value === null) return "Not available";
    return formatter.format(value) + " " + (value === 1 ? metric.slice(0, -1) : metric);
  }

  function element(tag, className, label) {
    const node = document.createElement(tag);
    node.className = className;
    if (label !== undefined) node.textContent = label;
    return node;
  }

  // One geometric mark per harness, drawn from local primitives so the same
  // identity travels from the usage ledger to the source folios. Nothing here
  // loads an external asset or a brand file.
  const solidGlyphs = { claude: true };
  const glyphShapes = {
    claude: [["path", { d: "" }]],
    codex: [
      ["path", { d: "M12 3.4 19.6 7.8v8.4L12 20.6 4.4 16.2V7.8Z" }],
      ["path", { d: "M12 7.2 16.2 9.6v4.8L12 16.8 7.8 14.4V9.6Z" }]
    ],
    hermes: [
      ["path", { d: "M12 3.6 20.8 20.4H3.2Z" }],
      ["path", { d: "M12 11.4 8.2 19.2h7.6Z" }]
    ],
    cursor: [
      ["path", { d: "M12 2.8 21 7.5v9L12 21.2 3 16.5v-9Z" }],
      ["path", { d: "M3 7.5 12 12.2l9-4.7M12 12.2v9" }]
    ],
    unknown: [
      ["path", { d: "M12 3.2 20.8 12 12 20.8 3.2 12Z" }],
      ["path", { d: "M12 8.6 15.4 12 12 15.4 8.6 12Z" }]
    ]
  };

  function starburst(rays, outer, inner) {
    const points = [];
    for (let index = 0; index < rays * 2; index += 1) {
      const radius = index % 2 === 0 ? outer : inner;
      const angle = (Math.PI * index) / rays - Math.PI / 2;
      points.push((12 + radius * Math.cos(angle)).toFixed(2) + " " + (12 + radius * Math.sin(angle)).toFixed(2));
    }
    return "M" + points.join("L") + "Z";
  }
  glyphShapes.claude = [["path", { d: starburst(12, 10, 3.4) }]];

  function glyph(harness, className) {
    const key = Object.prototype.hasOwnProperty.call(glyphShapes, harness) ? harness : "unknown";
    const base = className || "usage-glyph";
    const wrapper = element("span", base + (solidGlyphs[key] ? " " + base + "--solid" : ""));
    const svg = document.createElementNS(document.getElementById("svg-namespace-probe").namespaceURI, "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("focusable", "false");
    glyphShapes[key].forEach(function (shape) {
      const node = document.createElementNS(svg.namespaceURI, shape[0]);
      Object.keys(shape[1]).forEach(function (name) { node.setAttribute(name, String(shape[1][name])); });
      svg.append(node);
    });
    wrapper.setAttribute("aria-hidden", "true");
    wrapper.append(svg);
    return wrapper;
  }

  function render(container, providers) {
    if (!container) return;
    const entries = (Array.isArray(providers) ? providers : []).filter(function (provider) {
      return provider && typeof provider === "object" && !Array.isArray(provider);
    }).map(function (provider, index) {
      const harness = Object.prototype.hasOwnProperty.call(names, provider.id) ? provider.id : "unknown";
      return {
        key: index, harness: harness,
        name: text(provider.name, names[harness] || "Harness", 80),
        sessions: count(provider.sessions), prompts: count(provider.prompts),
        coverage: text(provider.coverage && provider.coverage.status, "Coverage assessment unavailable", 80)
      };
    });
    let metric = "sessions", selected = null, targets = [];
    const controls = element("div", "usage-controls");
    controls.setAttribute("role", "group");
    controls.setAttribute("aria-label", "Size harnesses by");
    const buttons = ["sessions", "prompts"].map(function (value) {
      const button = element("button", "usage-metric", value === "sessions" ? "Sessions" : "Prompts");
      button.type = "button";
      button.setAttribute("data-metric", value);
      button.addEventListener("click", function () { metric = value; update(); });
      controls.append(button);
      return button;
    });
    const visual = element("div", "usage-visual");
    const plot = element("div", "usage-plot");
    const key = element("ul", "usage-key");
    const detail = element("p", "usage-detail");
    detail.setAttribute("aria-live", "polite");
    detail.setAttribute("aria-atomic", "true");
    const note = element("p", "usage-note");
    visual.append(plot, key);
    container.replaceChildren(controls, visual, detail, note);

    function select(entry) {
      selected = entry.key;
      targets.forEach(function (target) {
        target.node.setAttribute("data-active", String(target.key === selected));
      });
      detail.textContent = entry.name + " · " + countLabel(entry[metric], metric) + " · " + entry.coverage + ".";
    }

    function bind(node, entry, keyboard) {
      targets.push({ key: entry.key, node: node });
      node.addEventListener("pointerenter", function () { select(entry); });
      node.addEventListener("click", function () { select(entry); });
      node.addEventListener("focus", function () { select(entry); });
      if (keyboard) node.addEventListener("keydown", function (event) {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          select(entry);
        }
      });
    }

    function update() {
      buttons.forEach(function (button, index) {
        button.setAttribute("aria-pressed", String((index === 0 ? "sessions" : "prompts") === metric));
      });
      const ordered = entries.slice().sort(function (a, b) {
        return (b[metric] === null ? -1 : b[metric]) - (a[metric] === null ? -1 : a[metric]) || a.key - b.key;
      });
      const positive = ordered.filter(function (entry) { return entry[metric] > 0; });
      const layout = positive.length <= 24 ? pack(positive.map(function (entry) { return entry[metric]; })) : null;
      // Use bars for sparse or dense distributions. Never enlarge a tiny circle
      // to make it tappable: that would silently invent magnitude.
      const packed = positive.length >= 3 && layout && layout.circles.every(function (circle) {
        return circle.r * 2 / Math.max(layout.width, layout.height) * 240 >= 44;
      });
      container.setAttribute("data-metric", metric);
      container.setAttribute("data-layout", packed ? "packed" : "bars");
      plot.replaceChildren();
      key.replaceChildren();
      targets = [];
      plot.hidden = !packed;
      const maximum = positive.length ? positive[0][metric] : 1;
      ordered.forEach(function (entry) {
        const row = element("li", "usage-key-row");
        const button = element("button", "usage-key-button");
        button.type = "button";
        button.setAttribute("data-harness", entry.harness);
        button.setAttribute("aria-label", entry.name + ": " + countLabel(entry[metric], metric) + ". " + entry.coverage);
        const label = element("span", "usage-key-name", entry.name);
        const value = element("span", "usage-key-value", entry[metric] === null ? "Not available" : formatter.format(entry[metric]));
        const other = metric === "sessions" ? "prompts" : "sessions";
        const secondary = element("span", "usage-key-secondary", countLabel(entry[other], other));
        button.append(glyph(entry.harness, "usage-glyph"), label, value);
        if (!packed && entry[metric] !== null) {
          const meter = element("meter", "usage-bar");
          meter.min = 0; meter.max = maximum; meter.value = entry[metric];
          meter.setAttribute("aria-hidden", "true");
          button.append(meter);
        }
        button.append(secondary, element("span", "usage-key-coverage", entry.coverage));
        bind(button, entry, false);
        row.append(button);
        key.append(row);
      });
      if (packed) {
        const ns = document.getElementById("svg-namespace-probe").namespaceURI;
        const svg = function (tag, attributes, label) {
          const node = document.createElementNS(ns, tag);
          Object.keys(attributes).forEach(function (name) { node.setAttribute(name, String(attributes[name])); });
          if (label !== undefined) node.textContent = label;
          return node;
        };
        const canvas = svg("svg", {
          viewBox: [layout.x, layout.y, layout.width, layout.height].join(" "),
          class: "usage-svg", role: "group", "aria-label": "Harnesses sized by " + metric
        });
        layout.circles.forEach(function (circle, index) {
          const entry = positive[index];
          const value = formatter.format(entry[metric]);
          const group = svg("g", {
            class: "usage-bubble", "data-harness": entry.harness, "data-count": entry[metric],
            role: "button", tabindex: "0",
            "aria-label": entry.name + ": " + countLabel(entry[metric], metric) + ". " + entry.coverage
          });
          group.append(svg("circle", { cx: circle.x, cy: circle.y, r: circle.r }));
          group.append(svg("text", {
            x: circle.x, y: circle.y, "text-anchor": "middle", "dominant-baseline": "central",
            class: "usage-bubble-value", "font-size": Math.min(layout.width * 0.07, circle.r * 1.7 / value.length),
            "aria-hidden": "true"
          }, value));
          bind(group, entry, true);
          canvas.append(group);
        });
        plot.append(canvas);
      }
      const basis = metric === "sessions" ? "Root sessions only; child sessions are separate." : "Recorded owner prompts.";
      note.textContent = (packed ? "Circle area represents " + metric + ". Colour identifies the harness. " :
        positive.length ? "Bars preserve exact counts for a sparse or uneven distribution. " : "No positive " + metric + " are available. ") +
        basis + " Recovered local history, not lifetime usage.";
      if (ordered.length) select(ordered.find(function (entry) { return entry.key === selected; }) || ordered[0]);
      else detail.textContent = "No harness records are available.";
    }
    update();
  }

  window.SkuggsjaUsageChart = { render: render, pack: pack, glyph: glyph };
}());
// END SKUGGSJA USAGE CHART

(function () {
  "use strict";

  const SVG_NS = document.getElementById("svg-namespace-probe").namespaceURI;
  const MAX_TEXT = 280;
  const MAX_CALENDAR_DAYS = 1100;
  const PRIMARY_LIST_LIMIT = 10;
  const numberFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
  const oneDecimalFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
  const monthNames = [
    "January", "February", "March", "April", "May", "June",
    "July", "August", "September", "October", "November", "December"
  ];
  const shortMonthNames = monthNames.map(function (month) { return month.slice(0, 3); });
  const weekdayNames = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];
  const MondayFirstRowLabels = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

  let activeController = null;
  let requestSerial = 0;

  const loadingState = document.getElementById("loading-state");
  const errorState = document.getElementById("error-state");
  const emptyState = document.getElementById("empty-state");
  const rewind = document.getElementById("rewind");
  const footer = document.querySelector(".page-footer");
  const chapterNav = document.querySelector(".folio-nav");
  const main = document.getElementById("main-content");

  document.getElementById("retry-button").addEventListener("click", function () { return loadRewind(true); });
  document.getElementById("empty-retry-button").addEventListener("click", function () { return loadRewind(true); });
  loadRewind();

  async function loadRewind(restoreFocus) {
    const serial = ++requestSerial;
    if (activeController) {
      activeController.abort();
    }
    activeController = new AbortController();
    const sourceActivity = document.getElementById("source-activity");
    sourceActivity.hidden = true;
    sourceActivity.textContent = "";
    showState("loading");
    main.setAttribute("aria-busy", "true");

    try {
      const response = await fetch("/api/rewind", {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        cache: "no-store",
        signal: activeController.signal
      });

      if (!response.ok) {
        throw new Error("The local report endpoint returned status " + response.status + ".");
      }

      const data = await response.json();
      if (!isRecord(data)) {
        throw new Error("The local report was not a JSON object.");
      }
      if (serial !== requestSerial) {
        return;
      }

      renderRewind(data);
    } catch (error) {
      if (serial !== requestSerial || (error && error.name === "AbortError")) {
        return;
      }
      showError(error);
    } finally {
      if (serial === requestSerial) {
        main.setAttribute("aria-busy", "false");
        if (restoreFocus) {
          const target = document.getElementById(!errorState.hidden ? "retry-button" : !emptyState.hidden ? "empty-title" : "hero-title");
          if (target.tagName !== "BUTTON") {
            target.setAttribute("tabindex", "-1");
          }
          target.focus();
        }
      }
    }
  }

  function renderRewind(data) {
    const totals = recordOrEmpty(data.totals);
    const sessions = numeric(totals.sessions, 0);
    renderFooter(data);
    renderSourceStatus(data);

    if (sessions <= 0) {
      renderEmptyState(data);
      showState("empty");
      return;
    }

    renderHero(data);
    renderActivity(data);
    renderRhythm(data);
    renderPromptStyle(data);
    window.SkuggsjaUsageChart.render(document.getElementById("usage-chart"), data.providers);
    renderModels(data.models, data.providers);
    renderProviders(data.providers);
    renderProjects(data.projects, data.longest_session, data.providers);
    renderPrivacy(data);
    renderMethodology(data.methodology, data.warnings);
    showState("rewind");
  }

  function renderHero(data) {
    const coverage = recordOrEmpty(data.coverage);
    const totals = recordOrEmpty(data.totals);
    const providers = arrayOrEmpty(data.providers);
    const sessions = numeric(totals.sessions, 0);
    const prompts = numeric(totals.prompts, 0);
    const projects = numeric(totals.projects, 0);
    const activeDays = numeric(totals.active_days, 0);
    const childSessions = numeric(totals.child_sessions, 0);
    const coverageLabel = cleanText(coverage.label, coverageRange(coverage), 80);
    setText("hero-edition", coverageLabel);
    setText("hero-session-count", formatNumber(sessions));
    setText("hero-session-label", "Top-level sessions · children counted separately");
    setText("proof-prompts", formatNumber(prompts));
    setText("proof-projects", formatNumber(projects));
    setText("proof-days", formatNumber(activeDays));
    setText("proof-prompts-note", "Counted from local transcripts, then discarded.");
    setText("proof-projects-note", "Distinct project names recovered.");
    setText("proof-days-note", "Days carrying at least one session.");
    renderProofSparks(data);
    renderHeroChart(recordOrEmpty(data.rhythm).activity);

    setText(
      "hero-narrative",
      "Your AI coding activity, as recorded on this machine. " +
      formatNumber(childSessions) + " child " + plural(childSessions, "session is", "sessions are") +
      " counted separately from the top-level total."
    );

    const range = coverageRange(coverage);
    const zone = cleanText(coverage.timezone, "local time", 80);
    const framing = coverage.calendar_framing === true ? "Calendar framing supported" : "Recorded span only";
    setText("coverage-stamp", framing + " · " + range + " · " + zone);

    const incompleteProviders = providers.filter(function (provider) {
      if (!isRecord(provider)) {
        return false;
      }
      const status = cleanText(recordOrEmpty(provider.coverage).status, "", 60).toLowerCase();
      return status === "known incomplete" || status === "coverage assessment incomplete" || status === "assessment unavailable";
    });
    const coverageNotice = document.getElementById("coverage-notice");
    coverageNotice.hidden = incompleteProviders.length === 0;
    if (incompleteProviders.length > 0) {
      setText(
        "coverage-notice-copy",
        formatNumber(incompleteProviders.length) + " " + plural(incompleteProviders.length, "harness has", "harnesses have") +
        " incomplete local-history " + plural(incompleteProviders.length, "assessment", "assessments") +
        ". Some records are proven missing or could not be fully assessed. Every total on this page means recoverable local records—not how little or how much the owner actually used a harness."
      );
    }

    setText("proof-audit", "Read-only");

    const sparse = sessions < 8 || activeDays < 3;
    const sparseNote = document.getElementById("sparse-note");
    sparseNote.hidden = !sparse;
    if (sparse) {
      setText(
        "sparse-copy",
        "Only " + formatNumber(sessions) + " " + plural(sessions, "session", "sessions") + " across " +
        formatNumber(activeDays) + " active " + plural(activeDays, "day is", "days are") +
        " represented. Patterns may be suggestive, but the interface will not promote them into a full-year claim."
      );
    }

    document.title = coverageLabel + " · skuggsja";
  }

  function renderActivity(data) {
    const coverage = recordOrEmpty(data.coverage);
    const rhythm = recordOrEmpty(data.rhythm);
    renderHeatmap(normalizeActivity(rhythm.activity), coverage);
  }

  // The hero chart and the two proof histograms draw only series the retained
  // aggregate actually carries: recorded sessions per day, and recorded
  // sessions per recovered project. Nothing is interpolated or invented.
  function renderProofSparks(data) {
    const byDate = new Map();
    normalizeActivity(recordOrEmpty(data.rhythm).activity).forEach(function (point) {
      byDate.set(point.date, point.count);
    });
    const days = Array.from(byDate.keys()).sort();
    if (days.length < 2) {
      return;
    }
    const first = parseDateOnly(days[0]);
    const last = parseDateOnly(days[days.length - 1]);
    const weeks = [];
    let cursor = addUTCDays(first, -mondayIndex(first));
    while (cursor.getTime() <= last.getTime()) {
      let active = 0;
      for (let day = 0; day < 7; day += 1) {
        if ((byDate.get(isoDate(addUTCDays(cursor, day))) || 0) > 0) {
          active += 1;
        }
      }
      weeks.push(active);
      cursor = addUTCDays(cursor, 7);
    }
    // Active days per week stay inside 0–7, so the historgram carries the real
    // shape without a single outlier flattening every other bar.
    drawSpark("proof-days-spark", weeks, "Active days per week across the recorded span", true);
  }

  function drawSpark(id, values, label, keepZeros) {
    const container = document.getElementById(id);
    if (!container) {
      return;
    }
    const series = (keepZeros ? values.slice(0, 96) : values.filter(function (value) { return value > 0; }).slice(0, 48));
    container.replaceChildren();
    if (series.length < 3) {
      container.hidden = true;
      return;
    }
    const maximum = Math.max.apply(null, series);
    const pitch = 4;
    const barWidth = 2.6;
    const height = 34;
    const clipped = values.length > series.length ? " Largest " + formatNumber(series.length) + " of " + formatNumber(values.length) + "." : "";
    const svg = svgElement("svg", {
      viewBox: "0 0 " + (series.length * pitch) + " " + height,
      preserveAspectRatio: "none",
      role: "img",
      "aria-label": label + "." + clipped
    });
    const floor = height - 2;
    series.forEach(function (value, index) {
      if (value <= 0) {
        return;
      }
      const barHeight = Math.max(1.5, (value / maximum) * floor);
      svg.appendChild(svgElement("rect", {
        x: String(index * pitch),
        y: String(height - barHeight),
        width: String(barWidth),
        height: String(barHeight),
        class: "spark-bar"
      }));
    });
    container.appendChild(svg);
    container.hidden = false;
  }

  function renderHeroChart(rawActivity) {
    const container = document.getElementById("hero-chart");
    if (!container) {
      return;
    }
    container.replaceChildren();
    const points = normalizeActivity(rawActivity);
    if (points.length < 2) {
      container.hidden = true;
      return;
    }

    const byDate = new Map();
    points.forEach(function (point) { byDate.set(point.date, point.count); });
    const first = parseDateOnly(points[0].date);
    const last = parseDateOnly(points[points.length - 1].date);
    const span = daysBetween(first, last) + 1;
    const weekly = span > 62;
    const buckets = [];
    if (weekly) {
      let cursor = addUTCDays(first, -mondayIndex(first));
      while (cursor.getTime() <= last.getTime()) {
        let count = 0;
        for (let day = 0; day < 7; day += 1) {
          count += byDate.get(isoDate(addUTCDays(cursor, day))) || 0;
        }
        buckets.push({ label: cursor.getTime() < first.getTime() ? first : cursor, count: count });
        cursor = addUTCDays(cursor, 7);
      }
    } else {
      for (let index = 0; index < span; index += 1) {
        const date = addUTCDays(first, index);
        buckets.push({ label: date, count: byDate.get(isoDate(date)) || 0 });
      }
    }

    const maximum = buckets.reduce(function (max, bucket) { return Math.max(max, bucket.count); }, 0);
    const total = points.reduce(function (sum, point) { return sum + point.count; }, 0);
    const positives = buckets
      .map(function (bucket) { return bucket.count; })
      .filter(function (count) { return count > 0; })
      .sort(function (left, right) { return left - right; });
    const median = positives.length > 0 ? positives[Math.floor(positives.length / 2)] : 0;
    // One bulk import beside a long tail flattens every other bar on a linear
    // axis, so a strongly skewed record switches to a labelled log scale.
    const logScale = positives.length >= 4 && maximum >= 20 && maximum >= median * 8;
    const scaleMaximum = logScale ? Math.pow(10, Math.ceil(Math.log10(maximum))) : niceMaximum(maximum);
    const fraction = function (value) {
      if (value <= 0) {
        return 0;
      }
      if (!logScale) {
        return value / scaleMaximum;
      }
      return Math.log10(value + 1) / Math.log10(scaleMaximum + 1);
    };
    const pitch = buckets.length > 60 ? 9 : 13;
    const barWidth = Math.max(2, pitch - 4.5);
    const gutter = 40;
    const plotTop = 8;
    const baseline = 112;
    const labelY = 126;
    const width = gutter + buckets.length * pitch + 4;
    const height = 132;
    const svg = svgElement("svg", {
      viewBox: "0 0 " + width + " " + height,
      role: "img",
      "aria-labelledby": "hero-chart-title hero-chart-description"
    });
    const busiest = points.reduce(function (best, point) { return point.count > best.count ? point : best; }, points[0]);
    svg.appendChild(svgElement("title", { id: "hero-chart-title" }, weekly ? "Recorded sessions per week" : "Recorded sessions per day"));
    svg.appendChild(svgElement(
      "desc",
      { id: "hero-chart-description" },
      formatNumber(total) + " recorded sessions across " +
      formatNumber(span) + " days" + (logScale ? ", drawn on a logarithmic scale" : "") +
      ". The busiest day, " + formatDateOnly(busiest.date) + ", held " +
      formatNumber(busiest.count) + " " + plural(busiest.count, "session", "sessions") + "."
    ));

    const gridLine = function (label, level) {
      const y = baseline - level * (baseline - plotTop);
      svg.appendChild(svgElement("line", { x1: String(gutter - 4), y1: String(y), x2: String(width - 2), y2: String(y), class: "hero-grid-line" }));
      svg.appendChild(svgElement("text", { x: String(gutter - 9), y: String(y + 3), "text-anchor": "end", class: "hero-axis-label" }, formatNumber(label)));
    };
    if (logScale) {
      for (let tick = 1; tick <= scaleMaximum; tick *= 10) {
        gridLine(tick, fraction(tick));
      }
    } else {
      [0, 1 / 3, 2 / 3, 1].forEach(function (level) { gridLine(Math.round(scaleMaximum * level), level); });
    }
    svg.appendChild(svgElement("line", { x1: String(gutter - 4), y1: String(baseline), x2: String(width - 2), y2: String(baseline), class: "hero-axis-line" }));

    buckets.forEach(function (bucket, index) {
      if (bucket.count <= 0) {
        return;
      }
      const barHeight = fraction(bucket.count) * (baseline - plotTop);
      const bar = svgElement("rect", {
        x: String(gutter + index * pitch + (pitch - barWidth) / 2),
        y: String(baseline - barHeight),
        width: String(barWidth),
        height: String(Math.max(1, barHeight)),
        class: "hero-bar"
      });
      bar.appendChild(svgElement(
        "title",
        {},
        (weekly ? "Week starting " : "") + formatDateOnly(isoDate(bucket.label)) + ": " +
        formatNumber(bucket.count) + " " + plural(bucket.count, "session", "sessions")
      ));
      svg.appendChild(bar);
    });

    const stride = Math.max(1, Math.ceil(buckets.length / 6));
    const labelIndexes = [];
    for (let index = 0; index < buckets.length; index += stride) {
      labelIndexes.push(index);
    }
    const lastIndex = buckets.length - 1;
    if (labelIndexes[labelIndexes.length - 1] !== lastIndex) {
      if (lastIndex - labelIndexes[labelIndexes.length - 1] >= stride * 0.5) {
        labelIndexes.push(lastIndex);
      } else {
        labelIndexes[labelIndexes.length - 1] = lastIndex;
      }
    }
    let lastLabelX = -Infinity;
    labelIndexes.forEach(function (index) {
      const x = gutter + index * pitch + pitch / 2;
      if (x - lastLabelX < 30) {
        return;
      }
      const date = buckets[index].label;
      svg.appendChild(svgElement("text", {
        x: String(x),
        y: String(labelY),
        "text-anchor": "middle",
        class: "hero-axis-label"
      }, shortMonthNames[date.getUTCMonth()] + " " + date.getUTCDate()));
      lastLabelX = x;
    });

    const busiestBucket = buckets.reduce(function (best, bucket) { return bucket.count > best.count ? bucket : best; }, buckets[0]);
    let caption = (weekly ? "Weekly totals" : "Daily totals") + (logScale ? " on a log scale (each line ×10)" : "");
    if (total > 0 && busiestBucket.count / total >= 0.5) {
      caption += ", and " + (weekly ? "the week of " : "") + formatDateOnly(isoDate(busiestBucket.label)) +
        " holds " + formatNumber(busiestBucket.count) + " of " + formatNumber(total) + " recorded sessions";
    }
    container.append(svg, element("p", "hero-chart__caption", caption + "."));
    container.hidden = false;
  }

  function niceMaximum(value) {
    if (value <= 5) {
      return Math.max(1, value);
    }
    const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
    const steps = [1, 1.5, 2, 2.5, 3, 4, 5, 6, 7.5, 10];
    for (let index = 0; index < steps.length; index += 1) {
      if (value <= steps[index] * magnitude) {
        return steps[index] * magnitude;
      }
    }
    return 10 * magnitude;
  }

  function renderHeatmap(activity, coverage) {
    const container = document.getElementById("activity-heatmap");
    container.replaceChildren();

    const range = calendarRange(activity, coverage);
    if (!range) {
      container.appendChild(emptyLedger("No dated activity was recorded by the available harnesses."));
      setText("activity-caption", "Calendar unavailable");
      return;
    }

    let start = range.start;
    const end = range.end;
    const fullDayCount = daysBetween(start, end) + 1;
    let clipped = false;
    if (fullDayCount > MAX_CALENDAR_DAYS) {
      start = addUTCDays(end, -(MAX_CALENDAR_DAYS - 1));
      clipped = true;
    }

    const activityMap = new Map();
    activity.forEach(function (point) {
      activityMap.set(point.date, point.count);
    });

    const firstWeekday = mondayIndex(start);
    const renderedDays = daysBetween(start, end) + 1;
    const columns = Math.ceil((renderedDays + firstWeekday) / 7);
    const cell = 12;
    const pitch = 13.5;
    const gutter = 34;
    const top = 16;
    const width = gutter + columns * pitch + 2;
    const height = top + 7 * pitch + 2;

    const days = [];
    let maximum = 0;
    let total = 0;
    for (let index = 0; index < renderedDays; index += 1) {
      const date = addUTCDays(start, index);
      const key = isoDate(date);
      const count = activityMap.get(key) || 0;
      days.push({ date: date, key: key, count: count, position: index + firstWeekday });
      maximum = Math.max(maximum, count);
      total += count;
    }

    const svg = svgElement("svg", {
      viewBox: "0 0 " + width + " " + height,
      width: String(width),
      height: String(height),
      role: "img",
      "aria-labelledby": "heatmap-title heatmap-description"
    });
    svg.appendChild(svgElement("title", { id: "heatmap-title" }, "Session activity calendar"));
    svg.appendChild(svgElement(
      "desc",
      { id: "heatmap-description" },
      formatNumber(total) + " recorded sessions across " + formatNumber(renderedDays) +
      " days. Darker marks indicate busier days."
    ));

    MondayFirstRowLabels.forEach(function (label, row) {
      svg.appendChild(svgElement("text", {
        x: "0",
        y: String(top + row * pitch + cell * 0.5 + 3),
        class: "heat-label"
      }, label));
    });

    let lastLabelColumn = -99;
    days.forEach(function (day) {
      if (day.position % 7 !== 0) {
        return;
      }
      const column = Math.round(day.position / 7);
      const monthStart = day.date.getUTCDate() === 1;
      if (column !== 0 && !monthStart && column - lastLabelColumn < 5) {
        return;
      }
      svg.appendChild(svgElement("text", {
        x: String(gutter + column * pitch),
        y: "10",
        class: monthStart || column === 0 ? "heat-month" : "heat-date"
      }, shortMonthNames[day.date.getUTCMonth()] + " " + day.date.getUTCDate()));
      lastLabelColumn = column;
    });

    days.forEach(function (day) {
      const column = Math.floor(day.position / 7);
      const row = day.position % 7;
      const rect = svgElement("rect", {
        x: String(gutter + column * pitch),
        y: String(top + row * pitch),
        width: String(cell),
        height: String(cell),
        rx: "1",
        class: "heat-cell heat-cell--" + heatLevel(day.count, maximum)
      });
      rect.appendChild(svgElement(
        "title",
        {},
        formatDateOnly(day.key) + ": " + formatNumber(day.count) + " " + plural(day.count, "session", "sessions")
      ));
      svg.appendChild(rect);
    });

    container.appendChild(svg);
    const caption = formatDateOnly(isoDate(start)) + " — " + formatDateOnly(isoDate(end));
    setText(
      "activity-caption",
      clipped ? caption + " · most recent " + formatNumber(MAX_CALENDAR_DAYS) + " days shown" : caption
    );
  }

  function renderRhythm(data) {
    const rhythm = recordOrEmpty(data.rhythm);
    const hours = fixedNumberArray(rhythm.hours, 24);
    const weekdays = fixedNumberArray(rhythm.weekdays, 7);

    renderClock(hours);
    renderWeekdays(weekdays);
    let busiest = 0;
    weekdays.forEach(function (count, index) {
      if (count > weekdays[busiest]) {
        busiest = index;
      }
    });
    const busiestCount = weekdays[busiest];
    setText("busiest-weekday", busiestCount > 0 ? weekdayNames[busiest] : "—");
    setText(
      "busiest-weekday-count",
      busiestCount > 0
        ? formatNumber(busiestCount) + " " + plural(busiestCount, "session", "sessions")
        : "no recorded sessions"
    );
    setText("favorite-hour", formatHour(rhythm.favorite_hour));
    setText("longest-streak", formatNumber(rhythm.longest_streak));
    setText(
      "late-night-percent",
      isFiniteNumber(rhythm.late_night_percent) ? formatDecimal(rhythm.late_night_percent) + "%" : "—"
    );
  }

  function renderClock(hours) {
    const container = document.getElementById("rhythm-clock");
    container.replaceChildren();
    const maximum = Math.max.apply(null, hours.concat([0]));
    const svg = svgElement("svg", {
      viewBox: "0 0 160 160",
      role: "img",
      "aria-labelledby": "clock-title clock-description"
    });
    svg.appendChild(svgElement("title", { id: "clock-title" }, "Sessions by hour of day"));
    svg.appendChild(svgElement(
      "desc",
      { id: "clock-description" },
      "A 24-hour radial plot. Longer marks indicate more sessions; vermilion marks are late-night hours."
    ));

    hours.forEach(function (count, hour) {
      const ratio = maximum > 0 ? Math.log1p(count) / Math.log1p(maximum) : 0;
      const x1 = 118;
      const x2 = 128 + ratio * 20;
      let level = "quiet";
      if (count > 0 && ratio < 0.38) { level = "low"; }
      if (ratio >= 0.38 && ratio < 0.72) { level = "medium"; }
      if (ratio >= 0.72) { level = "high"; }
      if (hour < 5 && count > 0) { level = "late"; }
      const line = svgElement("line", {
        x1: String(x1),
        y1: "80",
        x2: String(x2),
        y2: "80",
        transform: "rotate(" + (-90 + hour * 15) + " 80 80)",
        class: "clock-bar clock-bar--" + level
      });
      line.appendChild(svgElement(
        "title",
        {},
        String(hour).padStart(2, "0") + ":00 — " + formatNumber(count) + " " + plural(count, "session", "sessions")
      ));
      svg.appendChild(line);
    });

    [
      { value: "00", x: 80, y: 7, anchor: "middle" },
      { value: "06", x: 155, y: 82, anchor: "end" },
      { value: "12", x: 80, y: 158, anchor: "middle" },
      { value: "18", x: 5, y: 82, anchor: "start" }
    ].forEach(function (label) {
      svg.appendChild(svgElement("text", {
        x: String(label.x),
        y: String(label.y),
        "text-anchor": label.anchor,
        class: "clock-label"
      }, label.value));
    });
    container.appendChild(svg);
  }

  function renderWeekdays(counts) {
    const list = document.getElementById("weekday-list");
    list.replaceChildren();
    const maximum = Math.max.apply(null, counts.concat([1]));

    counts.forEach(function (count, index) {
      const item = document.createElement("li");
      const label = document.createElement("span");
      const meter = document.createElement("meter");
      const value = document.createElement("strong");
      label.textContent = weekdayNames[index];
      meter.min = 0;
      meter.max = maximum;
      meter.value = count;
      meter.setAttribute("aria-label", weekdayNames[index] + ": " + formatNumber(count) + " " + plural(count, "session", "sessions"));
      value.textContent = formatNumber(count);
      item.append(label, meter, value);
      list.appendChild(item);
    });
  }

  function renderPromptStyle(data) {
    const promptStyle = recordOrEmpty(data.prompt_style);
    const available = promptStyle.available === true;
    document.getElementById("prompt-unavailable").hidden = available;
    document.getElementById("prompt-available").hidden = !available;
    if (!available) {
      return;
    }

    setText("median-words", formatDecimal(promptStyle.median_words));
    setText("average-words", formatDecimal(promptStyle.average_words));
    setText("average-characters", formatDecimal(promptStyle.average_characters));
    setText("prompts-measured", formatNumber(promptStyle.count));
    setText(
      "prompt-label",
      cleanText(promptStyle.label, "Measured locally from prompt text, then discarded", 180)
    );
  }

  function renderModels(rawModels, rawProviders) {
    const container = document.getElementById("model-list");
    const providers = new Map();
    arrayOrEmpty(rawProviders).filter(isRecord).forEach(function (provider) {
      providers.set(cleanText(provider.id, "Unattributed harness", 80), cleanText(provider.name, provider.id, 90));
    });
    const groups = new Map();
    arrayOrEmpty(rawModels).filter(isRecord).forEach(function (model) {
      const harness = cleanText(model.harness, "Unattributed harness", 80);
      if (!groups.has(harness)) groups.set(harness, []);
      groups.get(harness).push({
        name: cleanText(model.name, "Unknown model", 110),
        meta: providers.get(harness) || harness,
        value: numeric(model.turns, 0),
        unit: "native events"
      });
    });
    container.replaceChildren();
    if (groups.size === 0) {
      container.appendChild(emptyLedger("No model-attributed events were recorded."));
      return;
    }
    Array.from(groups.keys()).sort().forEach(function (harness, index) {
      const section = element("details", "model-provider-index");
      section.open = index === 0;
      section.setAttribute("data-harness", harness);
      section.setAttribute("aria-label", (providers.get(harness) || harness) + " model events");
      const heading = element("summary", "model-provider-heading");
      heading.append(
        element("h4", "", providers.get(harness) || harness),
        element("span", "model-count", formatNumber(groups.get(harness).length) + " " + plural(groups.get(harness).length, "model", "models"))
      );
      section.appendChild(heading);
      const list = element("div", "");
      const models = groups.get(harness).sort(function (a, b) {
        return b.value - a.value || a.name.localeCompare(b.name);
      });
      renderRankedIndex(list, models, "No model-attributed events were recorded.");
      section.appendChild(list);
      container.appendChild(section);
    });
  }

  function renderProviders(rawProviders) {
    const container = document.getElementById("provider-list");
    const providers = arrayOrEmpty(rawProviders).filter(isRecord);
    container.replaceChildren();

    if (providers.length === 0) {
      container.appendChild(emptyLedger("No provider records were included in this report."));
      return;
    }

    let leadingIndex = 0;
    let maximumSessions = 0;
    providers.forEach(function (provider, index) {
      maximumSessions = Math.max(maximumSessions, numeric(provider.sessions, 0));
      if (numeric(provider.sessions, 0) > numeric(providers[leadingIndex].sessions, 0)) {
        leadingIndex = index;
      }
    });

    providers.forEach(function (provider, index) {
      container.appendChild(
        providerFolio(provider, index === leadingIndex && numeric(provider.sessions, 0) > 0, maximumSessions)
      );
    });
  }

  function harnessGlyph(id, className) {
    const chart = window.SkuggsjaUsageChart;
    return chart && typeof chart.glyph === "function" ? chart.glyph(id, className) : null;
  }

  function providerFolio(provider, shouldOpen, maximumSessions) {
    const details = document.createElement("details");
    const summary = document.createElement("summary");
    const title = element("span", "provider-title");
    const status = providerStatus(provider.status, numeric(provider.sessions, 0));
    const statusMark = element("span", "status-mark status-mark--" + status.kind, status.label);
    const name = element("strong", "", cleanText(provider.name, cleanText(provider.id, "Unknown harness", 80), 90));
    const sessions = element("span", "provider-session-count", formatNumber(provider.sessions));
    const sessionCell = element("span", "provider-session-cell");
    const sessionBar = document.createElement("meter");
    sessionBar.className = "provider-session-bar";
    sessionBar.min = 0;
    sessionBar.max = maximumSessions > 0 ? maximumSessions : 1;
    sessionBar.value = numeric(provider.sessions, 0);
    sessionBar.setAttribute("aria-hidden", "true");
    sessionBar.setAttribute("aria-label", cleanText(provider.name, "Harness", 90) + ": " + formatNumber(provider.sessions) + " recovered sessions");
    sessionCell.append(sessions, sessionBar);
    const verification = element(
      "span",
      "provider-verification",
      cleanText(provider.verification, "Unverified", 70)
    );
    const body = element("div", "provider-body");
    const facts = element("dl", "provider-facts");
    const tokenSection = element("section", "token-section");
    const notes = element("div", "provider-notes");
    const coverage = recordOrEmpty(provider.coverage);
    const coverageSection = providerCoverage(coverage);

    details.className = "provider-entry";
    details.setAttribute("data-harness", cleanText(provider.id, "unknown", 80));
    details.open = shouldOpen;
    const mark = harnessGlyph(provider.id, "provider-glyph");
    if (mark) {
      title.append(mark);
    }
    title.append(name, statusMark);
    summary.append(
      title, sessionCell, verification,
      element("span", "provider-summary-coverage", "Coverage: " + cleanText(coverage.status, "Completeness unknown", 70))
    );

    [
      ["Prompts", formatNumber(provider.prompts)],
      ["Projects", formatNumber(provider.projects)],
      ["Tool calls · native count", provider.tool_calls_available === true ? formatNumber(provider.tool_calls) : "Not available"],
      ["Child sessions", formatNumber(provider.child_sessions)],
      ["Span", providerSpan(provider)],
      ["Time basis", cleanText(provider.time_basis, "Not reported", 90)]
    ].forEach(function (fact) {
      facts.appendChild(definition(fact[0], fact[1]));
    });

    tokenSection.appendChild(element("h3", "", "Token ledger"));
    tokenSection.appendChild(renderTokenLedger(recordOrEmpty(provider.token_usage)));
    appendProviderNotes(notes, "Limitations", provider.limitations);
    appendProviderNotes(notes, "Parser notes", provider.warnings);
    body.append(coverageSection, facts, tokenSection);
    if (notes.childElementCount > 0) {
      body.appendChild(notes);
    }
    details.append(summary, body);
    return details;
  }

  function providerCoverage(coverage) {
    const status = cleanText(coverage.status, "Completeness unknown", 70);
    const confidence = cleanText(coverage.confidence, "not established", 40);
    const knownIncomplete = status.toLowerCase() === "known incomplete";
    const section = element("section", "provider-coverage" + (knownIncomplete ? " provider-coverage--incomplete" : ""));
    section.setAttribute("aria-label", "Local data coverage");
    const heading = element("div", "provider-coverage__heading");
    heading.append(
      element("h3", "", "Data coverage"),
      element("span", "coverage-status", status + " · " + confidence + " confidence")
    );
    section.appendChild(heading);
    section.appendChild(element(
      "p",
      "provider-coverage__note",
      cleanText(coverage.note, "Surviving local records do not establish account-lifetime completeness.", MAX_TEXT)
    ));
    const facts = element("dl", "coverage-facts");
    [
      ["Earliest local evidence", formatLocalInstantDate(coverage.earliest_local_evidence)],
      ["Earliest detailed record", formatLocalInstantDate(coverage.earliest_detailed_record)],
      ["History-only sessions", formatNumber(coverage.history_only_sessions)],
      ["Known refs without detail", formatNumber(coverage.unmaterialized_sessions)]
    ].forEach(function (fact) {
      facts.appendChild(definition(fact[0], fact[1]));
    });
    section.appendChild(facts);
    return section;
  }

  function renderTokenLedger(tokens) {
    const wrapper = element("div", "");
    if (tokens.available !== true) {
      const reason = cleanText(tokens.source, "This harness did not expose compatible token usage.", 180);
      wrapper.appendChild(element("p", "token-note", reason));
      return wrapper;
    }

    const precision = tokens.exact === true ? "Source-recorded ledger totals" : "Available counts; exactness not established";
    const source = cleanText(tokens.source, "source field not reported", 120);
    wrapper.appendChild(element("p", "token-note", precision + " · " + source + " · zero may mean recorded zero or an omitted/null category"));

    const scroll = element("div", "table-scroll");
    const table = element("table", "token-ledger");
    const caption = document.createElement("caption");
    const head = document.createElement("thead");
    const headRow = document.createElement("tr");
    const body = document.createElement("tbody");
    caption.className = "visually-hidden";
    caption.textContent = "Recorded token categories";
    headRow.append(element("th", "", "Category"), element("th", "", "Count"));
    head.appendChild(headRow);
    const categories = [
      ["Input", tokens.input],
      ["Output", tokens.output],
      ["Cache read", tokens.cache_read],
      ["Cache write", tokens.cache_write]
    ];
    categories.push(["Reasoning", tokens.reasoning]);
    categories.forEach(function (rowData) {
      const row = document.createElement("tr");
      row.append(element("td", "", rowData[0]), element("td", "", formatNumber(rowData[1])));
      body.appendChild(row);
    });
    table.append(caption, head, body);
    scroll.appendChild(table);
    wrapper.appendChild(scroll);
    return wrapper;
  }

  function appendProviderNotes(parent, heading, rawItems) {
    const items = arrayOrEmpty(rawItems).map(function (item) {
      if (typeof item === "string") {
        return cleanText(item, "Unspecified", MAX_TEXT);
      }
      if (!isRecord(item)) {
        return "";
      }
      const code = cleanText(item.code, "notice", 70);
      const count = numeric(item.count, 0);
      const message = cleanText(item.message, "No additional detail was reported.", MAX_TEXT);
      return code + (count > 0 ? " ×" + formatNumber(count) : "") + " — " + message;
    }).filter(Boolean);
    if (items.length === 0) {
      return;
    }
    const section = document.createElement("section");
    const list = document.createElement("ul");
    section.appendChild(element("h4", "", heading));
    items.forEach(function (item) {
      list.appendChild(element("li", "", cleanText(item, "Unspecified", MAX_TEXT)));
    });
    section.appendChild(list);
    parent.appendChild(section);
  }

  function renderProjects(rawProjects, rawLongestSession, rawProviders) {
    renderProjectsByTool(rawProviders);
    const projects = arrayOrEmpty(rawProjects)
      .filter(isRecord)
      .map(function (project) {
        return {
          name: cleanText(project.name, "Unnamed project", 110),
          meta: "project name only",
          value: numeric(project.sessions, 0),
          unit: "sessions"
        };
      })
      .sort(function (a, b) { return b.value - a.value || a.name.localeCompare(b.name); });
    renderRankedIndex(document.getElementById("project-list"), projects, "No privacy-safe project names were available.");

    const longest = recordOrEmpty(rawLongestSession);
    const available = longest.available === true && isFiniteNumber(longest.duration_minutes);
    setText("longest-session-duration", available ? formatDecimal(longest.duration_minutes) : "—");
    setText(
      "longest-session-harness",
      available ? cleanText(longest.harness, "Harness not attributed", 100) : "Duration unavailable from the surviving histories"
    );
  }

  // Sessions per harness, shown beside its project count. Tool-call units and
  // totals stay scoped to each harness and are never combined here.
  function renderProjectsByTool(rawProviders) {
    const container = document.getElementById("projects-by-tool");
    if (!container) {
      return;
    }
    const providers = arrayOrEmpty(rawProviders).filter(isRecord).map(function (provider) {
      return {
        name: cleanText(provider.name, cleanText(provider.id, "Unknown harness", 80), 90),
        projects: numeric(provider.projects, 0),
        sessions: numeric(provider.sessions, 0)
      };
    }).sort(function (a, b) {
      return b.sessions - a.sessions || a.name.localeCompare(b.name);
    });
    container.replaceChildren();
    if (providers.length === 0) {
      container.appendChild(emptyLedger("No harness records were included in this report."));
      return;
    }
    const maximum = Math.max.apply(null, providers.map(function (provider) { return provider.sessions; }).concat([1]));
    const head = element("p", "tool-head");
    head.append(element("span", "", "Tool"), element("span", "", "Projects"), element("span", "", "Sessions"));
    const list = element("ol", "tool-list");
    providers.forEach(function (provider) {
      const meter = document.createElement("meter");
      meter.min = 0;
      meter.max = maximum;
      meter.value = provider.sessions;
      meter.setAttribute(
        "aria-label",
        provider.name + ": " + formatNumber(provider.projects) + " " + plural(provider.projects, "project", "projects") +
        ", " + formatNumber(provider.sessions) + " " + plural(provider.sessions, "session", "sessions")
      );
      const row = element("li", "tool-row");
      row.append(
        element("span", "tool-row__name", provider.name),
        element("span", "tool-row__count", formatNumber(provider.projects)),
        element("span", "tool-row__count", formatNumber(provider.sessions) + " " + plural(provider.sessions, "session", "sessions")),
        meter
      );
      list.appendChild(row);
    });
    container.append(head, list);
  }

  function renderRankedIndex(container, items, emptyMessage) {
    container.replaceChildren();
    if (items.length === 0) {
      container.appendChild(emptyLedger(emptyMessage));
      return;
    }

    const maximum = Math.max.apply(null, items.map(function (item) { return item.value; }).concat([1]));
    const primary = document.createElement("ol");
    primary.className = "rank-list";
    items.slice(0, PRIMARY_LIST_LIMIT).forEach(function (item, index) {
      primary.appendChild(rankRow(item, index + 1, maximum));
    });
    container.appendChild(primary);

    if (items.length > PRIMARY_LIST_LIMIT) {
      const more = document.createElement("details");
      const summary = document.createElement("summary");
      const rest = document.createElement("ol");
      const collapse = document.createElement("button");
      const showMore = "Show " + formatNumber(items.length - PRIMARY_LIST_LIMIT) + " more";
      more.className = "more-index";
      summary.textContent = showMore;
      rest.className = "rank-list";
      rest.start = PRIMARY_LIST_LIMIT + 1;
      items.slice(PRIMARY_LIST_LIMIT).forEach(function (item, index) {
        rest.appendChild(rankRow(item, index + PRIMARY_LIST_LIMIT + 1, maximum));
      });
      collapse.type = "button";
      collapse.className = "more-index__collapse";
      collapse.textContent = "Show fewer";
      more.addEventListener("toggle", function () {
        summary.textContent = more.open ? "Show fewer" : showMore;
      });
      collapse.addEventListener("click", function () {
        more.open = false;
        summary.focus({ preventScroll: true });
        summary.scrollIntoView({ block: "nearest", behavior: "instant" });
      });
      more.append(summary, rest);
      if (items.length > PRIMARY_LIST_LIMIT * 2) more.appendChild(collapse);
      container.appendChild(more);
    }
  }

  function rankRow(item, position, maximum) {
    const row = document.createElement("li");
    const name = element("span", "rank-row__name");
    const meter = document.createElement("meter");
    const unit = item.value === 1 ? item.unit.replace(/s$/, "") : item.unit;
    row.className = "rank-row";
    name.append(element("strong", "", item.name), element("small", "", item.meta));
    meter.min = 0;
    meter.max = maximum;
    meter.value = item.value;
    meter.setAttribute("aria-label", item.name + ": " + formatNumber(item.value) + " " + unit);
    row.append(
      element("span", "rank-row__position", String(position).padStart(2, "0")),
      name,
      meter,
      element("span", "rank-row__value", formatNumber(item.value) + " " + unit)
    );
    return row;
  }

  function renderPrivacy(data) {
    const privacy = recordOrEmpty(data.privacy);
    const raw = privacyBoolean(privacy.raw_content_persisted, "No · discarded", "Yes · review required");
    const paths = privacyBoolean(privacy.absolute_paths_persisted, "No · stripped", "Yes · review required");
    const activity = sourceActivity(data);
    setPrivacyOutput("privacy-raw", raw.label, raw.good);
    setPrivacyOutput("privacy-paths", paths.label, paths.good);
    setText("privacy-source-access", "Read-only");
    setText("privacy-audit-files", activity.filesLabel);
    setText("audit-verdict", activity.summary);
    setText("privacy-source-activity", activity.detail);
  }

  function renderSourceStatus(data) {
    const activity = document.getElementById("source-activity");
    activity.textContent = sourceActivity(data).detail;
    activity.hidden = false;
  }

  function sourceActivity(data) {
    const privacy = recordOrEmpty(data.privacy);
    const audit = recordOrEmpty(privacy.source_audit);
    const observation = privacy.source_observation;
    if (observation === "disabled") {
      return {
        summary: "Observation skipped",
        detail: "Optional source activity observation was skipped. Source access remains read-only.",
        filesLabel: "Not observed"
      };
    }
    if (observation === "unavailable" || (observation !== "observed" && !auditWasCompared(audit))) {
      return {
        summary: "Observation unavailable",
        detail: "Source activity observation was unavailable. Source access remains read-only.",
        filesLabel: "Not observed"
      };
    }
    const changes = auditChangeCount(audit, recordOrEmpty(data.totals));
    const directories = changeCount(audit.directory_changes);
    const parts = [];
    if (changes > 0) {
      parts.push(formatNumber(changes) + " " + plural(changes, "file", "files") +
        " changed during the run by another process; skuggsja does not write to source paths.");
    }
    if (directories > 0) {
      parts.push(formatNumber(directories) + " directory " + plural(directories, "listing", "listings") +
        " changed during the run by another process.");
    }
    if (parts.length === 0) {
      parts.push("No concurrent source changes were observed during the run. Source access remains read-only.");
    }
    return {
      summary: changes > 0 || directories > 0 ? "Concurrent activity" : "No changes observed",
      detail: parts.join(" "),
      filesLabel: formatNumber(countValue(audit.files)) + " files"
    };
  }

  function renderMethodology(rawMethodology, rawWarnings) {
    const methodology = stringArray(rawMethodology);
    const warnings = arrayOrEmpty(rawWarnings).filter(isRecord);
    const methodList = document.getElementById("methodology-list");
    methodList.replaceChildren();
    if (methodology.length === 0) {
      methodList.appendChild(element("li", "", "No methodology notes were included in this report."));
    } else {
      methodology.forEach(function (item) {
        methodList.appendChild(element("li", "", cleanText(item, "Unspecified method", MAX_TEXT)));
      });
    }

    setText("warning-count", formatNumber(warnings.length));
    const warningList = document.getElementById("warning-list");
    warningList.replaceChildren();
    if (warnings.length === 0) {
      warningList.appendChild(element("p", "no-warnings", "✓ No parser warnings were reported."));
      return;
    }
    warnings.forEach(function (warning) {
      warningList.appendChild(warningRow(warning));
    });
  }

  function renderFooter(data) {
    const coverage = recordOrEmpty(data.coverage);
    setText("generated-at", formatDateTime(data.generated_at, coverage.timezone));
    setText("schema-version", cleanText(data.schema_version, "Not reported", 40));
    setText("generator-version", cleanText(data.generator_version, "Not reported", 40));
  }

  function renderEmptyState(data) {
    const coverage = recordOrEmpty(data.coverage);
    const providers = arrayOrEmpty(data.providers).filter(isRecord);
    const warnings = arrayOrEmpty(data.warnings).filter(isRecord);
    setText(
      "empty-copy",
      "No supported sessions landed inside " + coverageRange(coverage) + ". This is an empty result, not a zero-usage claim beyond that span."
    );

    const summary = document.getElementById("empty-provider-summary");
    summary.replaceChildren();
    if (providers.length === 0) {
      summary.appendChild(element("p", "", "No provider discovery records were included."));
    } else {
      providers.forEach(function (provider) {
        const status = providerStatus(provider.status, numeric(provider.sessions, 0));
        const entry = element("section", "empty-provider-entry");
        entry.append(
          element("h2", "", cleanText(provider.name, cleanText(provider.id, "Unknown harness", 80), 80) + " — " + status.label),
          providerCoverage(recordOrEmpty(provider.coverage))
        );
        summary.appendChild(entry);
      });
    }

    const warningContainer = document.getElementById("empty-warnings");
    warningContainer.replaceChildren();
    if (warnings.length === 0) {
      warningContainer.appendChild(element("p", "", "No discovery warnings were reported."));
    } else {
      warnings.forEach(function (warning) {
        warningContainer.appendChild(warningRow(warning));
      });
    }
    document.title = "Empty archive · skuggsja";
  }

  function showError(error) {
    const message = error instanceof Error ? error.message : "The local report endpoint did not answer.";
    setText("error-message", cleanText(message, "The local report endpoint did not answer.", 220));
    showState("error");
    document.title = "Archive unavailable · skuggsja";
  }

  function showState(state) {
    loadingState.hidden = state !== "loading";
    errorState.hidden = state !== "error";
    emptyState.hidden = state !== "empty";
    rewind.hidden = state !== "rewind";
    chapterNav.hidden = state !== "rewind";
    footer.hidden = state !== "rewind" && state !== "empty";
  }

  function warningRow(warning) {
    const row = element("div", "warning-row");
    const meta = element("div", "warning-row__meta");
    const harness = cleanText(warning.harness, "general", 70);
    const code = cleanText(warning.code, "warning", 70);
    const count = numeric(warning.count, 0);
    meta.append(
      element("span", "", harness),
      element("span", "", code),
      element("span", "", count > 0 ? "×" + formatNumber(count) : "")
    );
    row.append(meta, element("p", "", cleanText(warning.message, "No additional detail was reported.", MAX_TEXT)));
    return row;
  }

  function setPrivacyOutput(id, label, good) {
    const node = document.getElementById(id);
    if (!node) {
      return;
    }
    node.textContent = label;
    node.classList.remove("privacy-value--good", "privacy-value--warn");
    node.classList.add(good ? "privacy-value--good" : "privacy-value--warn");
  }

  function privacyBoolean(value, falseLabel, trueLabel) {
    if (value === false) {
      return { label: falseLabel, good: true };
    }
    if (value === true) {
      return { label: trueLabel, good: false };
    }
    return { label: "Not reported", good: false };
  }

  function providerStatus(rawStatus, sessions) {
    const status = cleanText(rawStatus, "unknown", 50).toLowerCase();
    if (["ok", "supported", "complete", "ready", "available"].includes(status)) {
      return { kind: "ok", label: "Supported" };
    }
    if (status === "supported with warnings") {
      return { kind: "partial", label: "Supported · warnings" };
    }
    if (["partial", "degraded", "warning", "limited"].includes(status)) {
      return { kind: "partial", label: "Partial" };
    }
    if (status === "unsupported schema") {
      return { kind: "error", label: "Unsupported schema" };
    }
    if (status === "unavailable") {
      return { kind: "error", label: "Unavailable" };
    }
    if (["error", "failed", "invalid"].includes(status)) {
      return { kind: "error", label: "Error" };
    }
    if (["missing", "not_found", "not found", "unsupported"].includes(status)) {
      return { kind: "missing", label: status === "unsupported" ? "Unsupported" : "Not found" };
    }
    if (sessions > 0) {
      return { kind: "partial", label: "Recorded" };
    }
    return { kind: "unknown", label: "Unknown" };
  }

  function providerSpan(provider) {
    const start = formatLocalInstantDate(provider.span_start);
    const end = formatLocalInstantDate(provider.span_end);
    if (start === "—" && end === "—") {
      return "Not reported";
    }
    if (start === end) {
      return start;
    }
    return start + " — " + end;
  }

  function coverageRange(coverage) {
    const label = cleanText(coverage.label, "", 80);
    if (label) {
      return label;
    }
    const start = formatLocalInstantDate(coverage.start);
    const end = formatLocalInstantDate(coverage.end);
    if (start === "—" && end === "—") {
      return "the recorded span";
    }
    if (start === end) {
      return start;
    }
    return start + " — " + end;
  }

  function normalizeActivity(rawActivity) {
    const merged = new Map();
    arrayOrEmpty(rawActivity).filter(isRecord).forEach(function (point) {
      const parsed = parseDateOnly(point.date);
      if (!parsed) {
        return;
      }
      const key = isoDate(parsed);
      merged.set(key, (merged.get(key) || 0) + numeric(point.count, 0));
    });
    return Array.from(merged.entries())
      .map(function (entry) { return { date: entry[0], count: entry[1] }; })
      .sort(function (a, b) { return a.date.localeCompare(b.date); });
  }

  function calendarRange(activity, coverage) {
    let start = null;
    let end = null;
    if (activity.length > 0) {
      start = parseDateOnly(activity[0].date);
      end = parseDateOnly(activity[activity.length - 1].date);
    }
    if (!start) {
      start = parseLocalInstantDate(coverage.start);
    }
    if (!end) {
      end = parseLocalInstantDate(coverage.end);
    }
    if (!start || !end) {
      return null;
    }
    if (start.getTime() > end.getTime()) {
      return { start: end, end: start };
    }
    return { start: start, end: end };
  }

  function heatLevel(count, maximum) {
    if (count <= 0 || maximum <= 0) {
      return 0;
    }
    const ratio = Math.log1p(count) / Math.log1p(maximum);
    return Math.max(1, Math.min(8, Math.ceil(ratio * 8)));
  }

  function auditChangeCount(audit, totals) {
    if (audit.changed_files !== undefined && audit.changed_files !== null) {
      return changeCount(audit.changed_files);
    }
    return numeric(totals.source_files_changed, 0);
  }

  function auditWasCompared(audit) {
    return typeof audit.manifest_before === "string" && audit.manifest_before !== "" &&
      typeof audit.manifest_after === "string" && audit.manifest_after !== "";
  }

  function changeCount(value) {
    if (Array.isArray(value)) {
      return value.length;
    }
    if (typeof value === "boolean") {
      return value ? 1 : 0;
    }
    return numeric(value, 0);
  }

  function countValue(value) {
    if (Array.isArray(value)) {
      return value.length;
    }
    return numeric(value, 0);
  }

  function formatHour(value) {
    if (!isFiniteNumber(value)) {
      return "—";
    }
    const hour = Math.trunc(Number(value));
    if (hour < 0 || hour > 23) {
      return "—";
    }
    return String(hour).padStart(2, "0") + ":00";
  }

  function formatDateOnly(value) {
    const date = parseDateOnly(value);
    if (!date) {
      return "—";
    }
    return shortMonthNames[date.getUTCMonth()] + " " + date.getUTCDate() + ", " + date.getUTCFullYear();
  }

  function formatLocalInstantDate(value) {
    if (typeof value !== "string" || value.trim() === "") {
      return "—";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) {
      return "—";
    }
    return shortMonthNames[date.getMonth()] + " " + date.getDate() + ", " + date.getFullYear();
  }

  function formatDateTime(value, timezone) {
    if (typeof value !== "string" || value.trim() === "") {
      return "Not reported";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return cleanText(value, "Not reported", 80);
    }
    const options = {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      timeZoneName: "short"
    };
    if (typeof timezone === "string" && timezone.trim() !== "") {
      options.timeZone = timezone;
    }
    try {
      return new Intl.DateTimeFormat(undefined, options).format(date);
    } catch (_) {
      delete options.timeZone;
      return new Intl.DateTimeFormat(undefined, options).format(date);
    }
  }

  function parseDateOnly(value) {
    if (typeof value !== "string") {
      return null;
    }
    const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(value.trim());
    if (!match) {
      return null;
    }
    const year = Number(match[1]);
    const month = Number(match[2]);
    const day = Number(match[3]);
    const date = new Date(Date.UTC(year, month - 1, day));
    if (
      date.getUTCFullYear() !== year ||
      date.getUTCMonth() !== month - 1 ||
      date.getUTCDate() !== day
    ) {
      return null;
    }
    return date;
  }

  function parseLocalInstantDate(value) {
    if (typeof value !== "string" || value.trim() === "") {
      return null;
    }
    const instant = new Date(value);
    if (Number.isNaN(instant.getTime()) || instant.getUTCFullYear() <= 1) {
      return null;
    }
    return new Date(Date.UTC(instant.getFullYear(), instant.getMonth(), instant.getDate()));
  }

  function addUTCDays(date, amount) {
    const next = new Date(date.getTime());
    next.setUTCDate(next.getUTCDate() + amount);
    return next;
  }

  function daysBetween(start, end) {
    return Math.round((end.getTime() - start.getTime()) / 86400000);
  }

  function mondayIndex(date) {
    return (date.getUTCDay() + 6) % 7;
  }

  function isoDate(date) {
    return date.toISOString().slice(0, 10);
  }

  function fixedNumberArray(value, length) {
    const source = Array.isArray(value) ? value : [];
    return Array.from({ length: length }, function (_, index) {
      return numeric(source[index], 0);
    });
  }

  function definition(term, description) {
    const wrapper = document.createElement("div");
    wrapper.append(element("dt", "", term), element("dd", "", cleanText(description, "—", 120)));
    return wrapper;
  }

  function emptyLedger(message) {
    const wrapper = element("div", "empty-ledger");
    wrapper.appendChild(element("p", "", message));
    return wrapper;
  }

  function setText(id, value) {
    const node = document.getElementById(id);
    if (node) {
      node.textContent = value === undefined || value === null ? "—" : String(value);
    }
  }

  function element(tagName, className, textValue) {
    const node = document.createElement(tagName);
    if (className) {
      node.className = className;
    }
    if (textValue !== undefined && textValue !== null) {
      node.textContent = String(textValue);
    }
    return node;
  }

  function svgElement(tagName, attributes, textValue) {
    const node = document.createElementNS(SVG_NS, tagName);
    Object.keys(attributes).forEach(function (name) {
      node.setAttribute(name, String(attributes[name]));
    });
    if (textValue !== undefined && textValue !== null) {
      node.textContent = String(textValue);
    }
    return node;
  }

  function cleanText(value, fallback, maximumLength) {
    if (value === undefined || value === null) {
      return fallback;
    }
    let text = String(value)
      .replace(/[\u0000-\u001f\u007f]+/g, " ")
      .replace(/\/(?:Users|home)\/[^\s,;]+/g, "[local path]")
      .replace(/[A-Za-z]:\\[^\s,;]+/g, "[local path]")
      .replace(/\s+/g, " ")
      .trim();
    if (text === "") {
      return fallback;
    }
    const limit = maximumLength || MAX_TEXT;
    if (text.length > limit) {
      text = text.slice(0, Math.max(1, limit - 1)).trimEnd() + "…";
    }
    return text;
  }

  function formatNumber(value) {
    return isFiniteNumber(value) ? numberFormatter.format(Number(value)) : "—";
  }

  function formatDecimal(value) {
    return isFiniteNumber(value) ? oneDecimalFormatter.format(Number(value)) : "—";
  }

  function numeric(value, fallback) {
    return isFiniteNumber(value) ? Math.max(0, Number(value)) : fallback;
  }

  function isFiniteNumber(value) {
    return value !== "" && value !== null && value !== undefined && Number.isFinite(Number(value));
  }

  function plural(value, singular, pluralValue) {
    return Number(value) === 1 ? singular : pluralValue;
  }

  function isRecord(value) {
    return value !== null && typeof value === "object" && !Array.isArray(value);
  }

  function recordOrEmpty(value) {
    return isRecord(value) ? value : {};
  }

  function arrayOrEmpty(value) {
    return Array.isArray(value) ? value : [];
  }

  function stringArray(value) {
    return arrayOrEmpty(value).filter(function (item) { return typeof item === "string"; });
  }
}());

// BEGIN SKUGGSJA FOLIO RAIL
(function () {
  "use strict";

  const nav = document.querySelector(".folio-nav");
  if (!nav || typeof nav.querySelectorAll !== "function" || typeof IntersectionObserver !== "function") {
    return;
  }
  const rewind = document.getElementById("rewind");
  if (!rewind || typeof rewind.querySelectorAll !== "function") {
    return;
  }
  const sections = Array.prototype.slice.call(rewind.querySelectorAll("section[id]"));
  const links = new Map();
  Array.prototype.forEach.call(nav.querySelectorAll('a[href^="#"]'), function (link) {
    const id = (link.getAttribute("href") || "").slice(1);
    if (id) {
      links.set(id, link);
    }
  });
  if (!sections.length || !links.size) {
    return;
  }

  const onScreen = new Set();
  function sync() {
    links.forEach(function (link) { link.removeAttribute("aria-current"); });
    const crossed = sections.filter(function (section) { return onScreen.has(section.id); });
    const current = links.get(crossed.length ? crossed[crossed.length - 1].id : sections[0].id);
    if (current) {
      current.setAttribute("aria-current", "true");
    }
  }
  const observer = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      if (entry.isIntersecting) {
        onScreen.add(entry.target.id);
      } else {
        onScreen.delete(entry.target.id);
      }
    });
    sync();
  }, { rootMargin: "-40% 0px -55% 0px", threshold: 0 });
  sections.forEach(function (section) { observer.observe(section); });
}());
// END SKUGGSJA FOLIO RAIL

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

  let activeController = null;
  let requestSerial = 0;
  let revealObserver = null;

  const loadingState = document.getElementById("loading-state");
  const errorState = document.getElementById("error-state");
  const emptyState = document.getElementById("empty-state");
  const rewind = document.getElementById("rewind");
  const footer = document.querySelector(".page-footer");
  const main = document.getElementById("main-content");

  document.getElementById("retry-button").addEventListener("click", loadRewind);
  document.getElementById("empty-retry-button").addEventListener("click", loadRewind);
  loadRewind();

  async function loadRewind() {
    const serial = ++requestSerial;
    if (activeController) {
      activeController.abort();
    }
    activeController = new AbortController();
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
      }
    }
  }

  function renderRewind(data) {
    const totals = recordOrEmpty(data.totals);
    const sessions = numeric(totals.sessions, 0);
    renderFooter(data);

    if (sessions <= 0) {
      renderEmptyState(data);
      showState("empty");
      return;
    }

    renderHero(data);
    renderActivity(data);
    renderRhythm(data);
    renderPromptStyle(data);
    renderModels(data.models);
    renderProviders(data.providers);
    renderProjects(data.projects, data.longest_session);
    renderPrivacy(data);
    renderMethodology(data.methodology, data.warnings);
    showState("rewind");
    prepareReveals();
  }

  function renderHero(data) {
    const coverage = recordOrEmpty(data.coverage);
    const totals = recordOrEmpty(data.totals);
    const privacy = recordOrEmpty(data.privacy);
    const audit = recordOrEmpty(privacy.source_audit);
    const providers = arrayOrEmpty(data.providers);
    const models = arrayOrEmpty(data.models);
    const sessions = numeric(totals.sessions, 0);
    const prompts = numeric(totals.prompts, 0);
    const projects = numeric(totals.projects, 0);
    const activeDays = numeric(totals.active_days, 0);
    const childSessions = numeric(totals.child_sessions, 0);
    const coverageLabel = cleanText(coverage.label, coverageRange(coverage), 80);
    const leadingProvider = providers
      .filter(isRecord)
      .slice()
      .sort(function (a, b) { return numeric(b.sessions, 0) - numeric(a.sessions, 0); })[0];
    const leadingModel = models
      .filter(isRecord)
      .slice()
      .sort(function (a, b) { return numeric(b.turns, 0) - numeric(a.turns, 0); })[0];

    setText("hero-edition", "Personal archive · " + coverageLabel);
    setText("hero-session-count", formatNumber(sessions));
    setText("hero-session-label", sessions === 1 ? "session held in this mirror" : "sessions held in this mirror");
    setText("proof-prompts", formatNumber(prompts));
    setText("proof-projects", formatNumber(projects));
    setText("proof-days", formatNumber(activeDays));

    const clauses = [];
    clauses.push(
      "Across " + formatNumber(activeDays) + " active " + plural(activeDays, "day", "days") +
      ", the surviving histories hold " + formatNumber(sessions) + " " + plural(sessions, "session", "sessions") + "."
    );
    if (leadingProvider && numeric(leadingProvider.sessions, 0) > 0) {
      clauses.push(cleanText(leadingProvider.name, cleanText(leadingProvider.id, "One harness", 80), 80) + " carried the largest recovered share.");
    }
    if (leadingModel && numeric(leadingModel.turns, 0) > 0) {
      clauses.push(cleanText(leadingModel.name, "The leading model", 100) + " appears most often in model-attributed events.");
    }
    if (childSessions > 0) {
      clauses.push(formatNumber(childSessions) + " child " + plural(childSessions, "session was", "sessions were") + " recorded alongside that total and remain separately labeled.");
    }
    clauses.push("This is a portrait of what remained on this machine, not a claim about anything outside the recorded span.");
    setText("hero-narrative", clauses.join(" "));

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

    const auditChanges = auditChangeCount(audit, totals) + changeCount(audit.directory_changes);
    if (audit.verified === true) {
      setText("proof-audit", "0 changed · verified");
    } else if (auditWasCompared(audit) && auditChanges > 0) {
      setText("proof-audit", formatNumber(auditChanges) + " changed · detected");
    } else {
      setText("proof-audit", "Not verified");
    }

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
    const totals = recordOrEmpty(data.totals);
    const rhythm = recordOrEmpty(data.rhythm);
    const busiestDay = recordOrEmpty(rhythm.busiest_day);
    const busiestMonth = recordOrEmpty(rhythm.busiest_month);
    const activity = normalizeActivity(rhythm.activity);

    renderHeatmap(activity, coverage);
    setText("busiest-day-date", formatDateOnly(busiestDay.date));
    setText(
      "busiest-day-count",
      numeric(busiestDay.count, 0) > 0
        ? formatNumber(busiestDay.count) + " " + plural(numeric(busiestDay.count, 0), "session", "sessions")
        : "No recorded sessions"
    );
    setText("busiest-month-label", cleanText(busiestMonth.label, "—", 60));
    setText(
      "busiest-month-count",
      numeric(busiestMonth.count, 0) > 0
        ? formatNumber(busiestMonth.count) + " " + plural(numeric(busiestMonth.count, 0), "session", "sessions")
        : "No recorded sessions"
    );
    setText("source-file-count", formatNumber(totals.source_files));
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

    const visibleActivity = activity.filter(function (point) {
      const date = parseDateOnly(point.date);
      return date && date.getTime() >= start.getTime() && date.getTime() <= end.getTime();
    });
    const activityMap = new Map();
    visibleActivity.forEach(function (point) {
      activityMap.set(point.date, point.count);
    });

    const firstWeekday = mondayIndex(start);
    const renderedDays = daysBetween(start, end) + 1;
    const columns = Math.ceil((renderedDays + firstWeekday) / 7);
    const width = Math.max(360, 32 + columns * 12);
    const height = 118;
    const maximum = visibleActivity.reduce(function (max, point) { return Math.max(max, point.count); }, 0);
    const total = visibleActivity.reduce(function (sum, point) { return sum + point.count; }, 0);
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
      formatNumber(total) + " recorded sessions across the displayed calendar range. Darker cobalt marks indicate busier days."
    ));

    [
      { label: "Mon", row: 0 },
      { label: "Wed", row: 2 },
      { label: "Fri", row: 4 }
    ].forEach(function (item) {
      svg.appendChild(svgElement("text", { x: "0", y: String(34 + item.row * 12), class: "heat-label" }, item.label));
    });

    let lastMonth = -1;
    for (let index = 0; index < renderedDays; index += 1) {
      const date = addUTCDays(start, index);
      const key = isoDate(date);
      const count = activityMap.get(key) || 0;
      const position = index + firstWeekday;
      const column = Math.floor(position / 7);
      const row = position % 7;
      const x = 30 + column * 12;
      const y = 28 + row * 12;
      const level = heatLevel(count, maximum);

      if (date.getUTCMonth() !== lastMonth && (index === 0 || date.getUTCDate() <= 7)) {
        svg.appendChild(svgElement(
          "text",
          { x: String(x), y: "12", class: "heat-month" },
          shortMonthNames[date.getUTCMonth()]
        ));
        lastMonth = date.getUTCMonth();
      }

      const rect = svgElement("rect", {
        x: String(x),
        y: String(y),
        width: "9",
        height: "9",
        rx: "0.7",
        class: "heat-cell heat-cell--" + level
      });
      rect.appendChild(svgElement(
        "title",
        {},
        formatDateOnly(key) + ": " + formatNumber(count) + " " + plural(count, "session", "sessions")
      ));
      svg.appendChild(rect);
    }

    container.appendChild(svg);
    const caption = formatDateOnly(isoDate(start)) + " — " + formatDateOnly(isoDate(end));
    setText(
      "activity-caption",
      clipped ? caption + " · most recent " + formatNumber(MAX_CALENDAR_DAYS) + " days shown" : caption
    );
  }

  function renderRhythm(data) {
    const rhythm = recordOrEmpty(data.rhythm);
    const totals = recordOrEmpty(data.totals);
    const hours = fixedNumberArray(rhythm.hours, 24);
    const weekdays = fixedNumberArray(rhythm.weekdays, 7);

    renderClock(hours);
    renderWeekdays(weekdays);
    setText("favorite-hour", formatHour(rhythm.favorite_hour));
    setText("longest-streak", formatNumber(rhythm.longest_streak));
    setText(
      "late-night-percent",
      isFiniteNumber(rhythm.late_night_percent) ? formatDecimal(rhythm.late_night_percent) + "%" : "—"
    );
    setText("tool-call-count", formatNumber(totals.tool_calls));
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
      meter.setAttribute("aria-label", weekdayNames[index] + ": " + formatNumber(count) + " sessions");
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

  function renderModels(rawModels) {
    const models = arrayOrEmpty(rawModels)
      .filter(isRecord)
      .map(function (model) {
        return {
          name: cleanText(model.name, "Unknown model", 110),
          meta: cleanText(model.harness, "Unattributed harness", 80),
          value: numeric(model.turns, 0),
          unit: "events"
        };
      })
      .sort(function (a, b) { return b.value - a.value || a.name.localeCompare(b.name); });
    renderRankedIndex(document.getElementById("model-list"), models, "No model-attributed events were recorded.");
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
    providers.forEach(function (provider, index) {
      if (numeric(provider.sessions, 0) > numeric(providers[leadingIndex].sessions, 0)) {
        leadingIndex = index;
      }
    });

    providers.forEach(function (provider, index) {
      container.appendChild(providerFolio(provider, index === leadingIndex && numeric(provider.sessions, 0) > 0));
    });
  }

  function providerFolio(provider, shouldOpen) {
    const details = document.createElement("details");
    const summary = document.createElement("summary");
    const title = element("span", "provider-title");
    const status = providerStatus(provider.status, numeric(provider.sessions, 0));
    const statusMark = element("span", "status-mark status-mark--" + status.kind, status.label);
    const name = element("strong", "", cleanText(provider.name, cleanText(provider.id, "Unknown harness", 80), 90));
    const sessions = element("span", "provider-session-count", formatNumber(provider.sessions));
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
    details.open = shouldOpen;
    title.append(statusMark, name);
    summary.append(title, sessions, verification);

    [
      ["Prompts", formatNumber(provider.prompts)],
      ["Projects", formatNumber(provider.projects)],
      ["Tool calls", provider.tool_calls_available === true ? formatNumber(provider.tool_calls) : "Not available"],
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

  function renderProjects(rawProjects, rawLongestSession) {
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
      more.className = "more-index";
      summary.textContent = "Show " + formatNumber(items.length - PRIMARY_LIST_LIMIT) + " more";
      rest.className = "rank-list";
      rest.start = PRIMARY_LIST_LIMIT + 1;
      items.slice(PRIMARY_LIST_LIMIT).forEach(function (item, index) {
        rest.appendChild(rankRow(item, index + PRIMARY_LIST_LIMIT + 1, maximum));
      });
      more.append(summary, rest);
      container.appendChild(more);
    }
  }

  function rankRow(item, position, maximum) {
    const row = document.createElement("li");
    const name = element("span", "rank-row__name");
    const meter = document.createElement("meter");
    row.className = "rank-row";
    name.append(element("strong", "", item.name), element("small", "", item.meta));
    meter.min = 0;
    meter.max = maximum;
    meter.value = item.value;
    meter.setAttribute("aria-label", item.name + ": " + formatNumber(item.value) + " " + item.unit);
    row.append(
      element("span", "rank-row__position", String(position).padStart(2, "0")),
      name,
      meter,
      element("span", "rank-row__value", formatNumber(item.value) + " " + item.unit)
    );
    return row;
  }

  function renderPrivacy(data) {
    const totals = recordOrEmpty(data.totals);
    const privacy = recordOrEmpty(data.privacy);
    const audit = recordOrEmpty(privacy.source_audit);
    const raw = privacyBoolean(privacy.raw_content_persisted, "No · discarded", "Yes · review required");
    const paths = privacyBoolean(privacy.absolute_paths_persisted, "No · stripped", "Yes · review required");
    const changes = auditChangeCount(audit, totals);
    const directories = changeCount(audit.directory_changes);
    const auditFiles = countValue(audit.files);
    const compared = auditWasCompared(audit);
    const unchanged = audit.verified === true && changes === 0 && directories === 0;
    setPrivacyOutput("privacy-raw", raw.label, raw.good);
    setPrivacyOutput("privacy-paths", paths.label, paths.good);
    setPrivacyOutput(
      "privacy-changed",
      compared ? formatNumber(changes + directories) : "Not verified",
      unchanged
    );
    setPrivacyOutput(
      "privacy-audit-files",
      compared ? formatNumber(auditFiles) + " files" : "Not verified",
      audit.verified === true
    );

    let verdict = "Not verified";
    if (audit.verified === true && unchanged) {
      verdict = "Verified unchanged";
    } else if (compared && changes + directories > 0) {
      verdict = "Changes detected";
    } else if (compared) {
      verdict = "Inconclusive";
    }
    setText("audit-verdict", verdict);
    setText("manifest-before", compactHash(audit.manifest_before));
    setText("manifest-after", compactHash(audit.manifest_after));
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
        summary.appendChild(element(
          "p",
          "",
          cleanText(provider.name, cleanText(provider.id, "Unknown harness", 80), 80) + " — " + status.label +
          " · coverage: " + cleanText(recordOrEmpty(provider.coverage).status, "unknown", 70)
        ));
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
    footer.hidden = state !== "rewind" && state !== "empty";
    if (state === "loading" && revealObserver) {
      revealObserver.disconnect();
    }
  }

  function prepareReveals() {
    if (revealObserver) {
      revealObserver.disconnect();
    }
    const chapters = Array.from(document.querySelectorAll("#rewind .chapter"));
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reducedMotion || !("IntersectionObserver" in window)) {
      chapters.forEach(function (chapter) { chapter.classList.add("is-visible"); });
      return;
    }

    chapters.forEach(function (chapter) {
      chapter.classList.add("will-reveal");
      chapter.classList.remove("is-visible");
    });
    revealObserver = new IntersectionObserver(function (entries, observer) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
      });
    }, { rootMargin: "0px 0px -8% 0px", threshold: 0.08 });
    chapters.forEach(function (chapter) { revealObserver.observe(chapter); });
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
    return Math.max(1, Math.min(4, Math.ceil(ratio * 4)));
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

  function compactHash(value) {
    if (typeof value !== "string" || value.trim() === "") {
      return "Not recorded";
    }
    const hash = cleanText(value, "Not recorded", 128);
    if (hash.length <= 28) {
      return hash;
    }
    return hash.slice(0, 16) + "…" + hash.slice(-8);
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

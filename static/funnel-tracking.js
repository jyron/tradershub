(function () {
  var storageKey = "bottrade_attribution_v1";
  var allowed = ["utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term", "ref"];

  function clean(value) {
    return String(value || "").trim().slice(0, 160);
  }

  function readAttribution() {
    try {
      return JSON.parse(window.localStorage.getItem(storageKey) || "{}") || {};
    } catch (_) {
      return {};
    }
  }

  function writeAttribution(value) {
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(value));
    } catch (_) {}
  }

  var params = new URLSearchParams(window.location.search);
  var attribution = readAttribution();
  var changed = false;
  allowed.forEach(function (key) {
    if (params.has(key) && clean(params.get(key))) {
      attribution[key] = clean(params.get(key));
      changed = true;
    }
  });
  if (changed || !attribution.first_landing_path) {
    attribution.first_landing_path = attribution.first_landing_path || window.location.pathname;
    attribution.first_seen_at = attribution.first_seen_at || new Date().toISOString();
    writeAttribution(attribution);
  }

  var campaignProps = {};
  Object.keys(attribution).forEach(function (key) {
    campaignProps["bt_" + key] = attribution[key];
  });
  if (window.posthog && posthog.register) posthog.register(campaignProps);

  function capture(event, properties) {
    if (!window.posthog || !posthog.capture) return;
    posthog.capture(event, Object.assign({
      path: window.location.pathname,
      page_title: document.title
    }, campaignProps, properties || {}));
  }

  window.bottradeTrack = capture;

  var funnelPaths = ["/", "/builders", "/login", "/account", "/challenge", "/pricing", "/leaderboard"];
  if (funnelPaths.indexOf(window.location.pathname) !== -1) {
    capture("funnel_page_viewed", { funnel_step: window.location.pathname });
  }
  if (window.location.pathname === "/builders") {
    capture("builder_landing_viewed");
  }

  document.addEventListener("click", function (event) {
    var target = event.target.closest("[data-track]");
    if (!target) return;
    capture("funnel_cta_clicked", {
      cta: clean(target.getAttribute("data-track")),
      destination: clean(target.getAttribute("href") || target.getAttribute("data-destination")),
      placement: clean(target.getAttribute("data-placement"))
    });
  }, true);
}());

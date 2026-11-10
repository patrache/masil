// The one way to describe a desktop alert (i18n 7.3, api.md 2 "alert 템플릿"): what it says is a template + params,
// plus the user's own text for templates whose body is "verbatim". notify.ts renders the stored Korean fallback from
// it; UserHub re-renders title / body per connection language when it sends the frame.
import type { Alert, AlertKind, FocusSummary } from "../types/api";
import { type AlertTemplate, type Lang, renderAlert, isAlertTemplateId } from "./i18n";

export interface AlertSpec {
  kind: AlertKind;
  template: AlertTemplate;
  /** verbatim user text (already a plain snippet); "" for templates whose body is "none" / "template" */
  body: string;
  target: Alert["target"];
}

export function alertSpec(kind: AlertKind, template: AlertTemplate, target: Alert["target"], verbatimBody = ""): AlertSpec {
  return { kind, template, body: verbatimBody, target };
}

/**
 * `alert` with title / body rendered in `lang` from its template. An alert stored without a template (by an older
 * server) or with an id this server does not know is returned unchanged.
 */
export function localizeAlert(alert: Alert, lang: Lang): Alert {
  const t = alert.template;
  if (t === null || t === undefined || typeof t !== "object" || !isAlertTemplateId(t.id)) return alert;
  const r = renderAlert(t, lang, alert.body);
  return r.title === alert.title && r.body === alert.body ? alert : { ...alert, title: r.title, body: r.body };
}

/** FocusSummary as a request in `lang` sees it (DELETE /v1/me/focus, i18n 7.3). */
export function localizeSummary(summary: FocusSummary, lang: Lang): FocusSummary {
  return lang === "ko" ? summary : { ...summary, items: summary.items.map((it) => localizeAlert(it, lang)) };
}

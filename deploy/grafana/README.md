# Grafana dashboard

Product counters for hosted dead-drop. Import `dead-drop.json` into Grafana Cloud
(folder **dead-drop**, Prometheus datasource `grafanacloud-prom`). Not the
GrafanaCloud usage / billing folder.

Live: https://neatpiano2663.grafana.net/d/dead-drop-product/dead-drop

Creates, burns, Takes, 429s, 403s. No secret ids, IPs, or ciphertext. `/metrics`
stays off the public ingress.

Each replica must export a unique `service.instance.id` (pod hostname) or the
two pods overwrite one series and `increase()` over a day looks like thousands
of creates. That is in 0.1.4. Until the old series ages out of the time range,
24h stats can still look noisy.

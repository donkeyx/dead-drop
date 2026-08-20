# Grafana dashboard

Product counters for hosted dead-drop. Import `dead-drop.json` into Grafana Cloud
(folder **dead-drop**, Prometheus datasource `grafanacloud-prom`). Not the
GrafanaCloud usage / billing folder.

Live: https://neatpiano2663.grafana.net/d/dead-drop-product/dead-drop

Creates, burns, expired, passphrase, Takes, 429s, 403s. No secret ids, IPs, or ciphertext. `/metrics`
stays off the public ingress.

Each replica must export a unique `service.instance.id` (pod hostname) or the
two pods overwrite one series and `increase()` over a day looks like thousands
of creates. That is in 0.1.4. Durable UI totals (created/burned/expired) live
in Postgres `deaddrop.stats`, not in these OTLP series.

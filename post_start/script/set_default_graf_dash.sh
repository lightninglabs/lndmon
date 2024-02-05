#!/bin/sh

# Set your Grafana instance's connection configuration.
GRAFANA_USER=${GRAFANA_USER:-admin}
GRAFANA_PASS=${GRAFANA_PASS:-admin}
GRAFANA_HOST=${GRAFANA_HOST:-grafana}
GRAFANA_PORT=${GRAFANA_PORT:-3000}

echo "Setting Grafana default dashboard..."
DASH_UID="sJUFc-NWk"
DASH_ID=0
for i in 1 2 3 4 5; do
    curl -H 'Content-Type: application/json' -u "${GRAFANA_USER}:${GRAFANA_PASS}" -X GET http://${GRAFANA_HOST}:${GRAFANA_PORT}/api/dashboards/uid/${DASH_UID} && RESP=$(curl -H 'Content-Type: application/json' -u "${GRAFANA_USER}:${GRAFANA_PASS}" -X GET http://${GRAFANA_HOST}:${GRAFANA_PORT}/api/dashboards/uid/${DASH_UID}) && DASH_ID=$( echo "$RESP" | jq '.dashboard.id' ) && break || sleep 15;
done

for i in 1 2 3 4 5; do
    curl -d "{\"homeDashboardId\":${DASH_ID}}" -H 'Content-Type: application/json' -u "${GRAFANA_USER}:${GRAFANA_PASS}" -X PUT http://${GRAFANA_HOST}:${GRAFANA_PORT}/api/org/preferences && break || sleep 15;
done

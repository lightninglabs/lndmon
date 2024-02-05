#!/bin/sh

# Set your Grafana instance's connection configuration.
GRAFANA_USER=${GRAFANA_USER:-admin}
GRAFANA_PASS=${GRAFANA_PASS:-admin}
GRAFANA_HOST=${GRAFANA_HOST:-grafana}
GRAFANA_PORT=${GRAFANA_PORT:-3000}
GRAFANA_URI=${GRAFANA_USER}:${GRAFANA_PASS}@${GRAFANA_HOST}:${GRAFANA_PORT}

echo "Setting Grafana default dashboard..."
DASH_UID="sJUFc-NWk"
DASH_ID=0
for i in 1 2 3 4 5; do
    curl -H 'Content-Type: application/json' -X GET "http://${GRAFANA_URI}/api/dashboards/uid/$DASH_UID" && RESP=$(curl -H 'Content-Type: application/json' -X GET "http://${GRAFANA_URI}/api/dashboards/uid/$DASH_UID") && DASH_ID=$( echo "$RESP" | jq '.dashboard.id' ) && break || sleep 15;
done

for i in 1 2 3 4 5; do
    curl -d "{\"homeDashboardId\":$DASH_ID}" -H 'Content-Type: application/json' -X PUT "http://${GRAFANA_URI}/api/org/preferences" && break || sleep 15;
done

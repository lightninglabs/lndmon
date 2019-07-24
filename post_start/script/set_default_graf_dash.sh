#!/bin/sh

echo "Setting Grafana default dashboard..."
DASH_UID="sJUFc-NWk"
DASH_ID=0
for i in 1 2 3 4 5; do
    curl -H 'Content-Type: application/json' -X GET http://admin:admin@grafana:3000/api/dashboards/uid/$DASH_UID && RESP=$(curl -H 'Content-Type: application/json' -X GET http://admin:admin@grafana:3000/api/dashboards/uid/$DASH_UID) && DASH_ID=$( echo "$RESP" | jq '.dashboard.id' ) && break || sleep 15;
done

for i in 1 2 3 4 5; do
    curl -d "{\"homeDashboardId\":$DASH_ID}" -H 'Content-Type: application/json' -X PUT http://admin:admin@grafana:3000/api/org/preferences && break || sleep 15;
done

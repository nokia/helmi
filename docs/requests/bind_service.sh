#!/bin/sh

curl -i -X "PUT" "http://localhost:5000/v2/service_instances/3b2e7d2c915242a5befcf03e1c3f47cd/service_bindings/09a22eb6c23c4a33b074b7ef082a5759" \
     -H "X-Broker-API-Version: 2.13" \
     -H "Content-Type: application/json; charset=utf-8" \
     -d $'{ "plan_id": "e79306ef-4e10-4e3d-b38e-ffce88c90f59", "service_id": "ab53df4d-c279-4880-94f7-65e7d72b7834" }'

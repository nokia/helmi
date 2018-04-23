#!/bin/sh

curl -i -X "DELETE" -H "X-Broker-API-Version: 2.13" \
     "http://localhost:5000/v2/service_instances/3b2e7d2c915242a5befcf03e1c3f47cd?service_id=ab53df4d-c279-4880-94f7-65e7d72b7834&plan_id=e79306ef-4e10-4e3d-b38e-ffce88c90f59&accepts_incomplete=true"

#!/bin/sh

curl -ss -H "X-Broker-API-Version: 2.13" "http://localhost:5000/v2/catalog" | json_pp

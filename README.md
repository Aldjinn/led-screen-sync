# led-screen-sync

```bash
dennis@DESKTOP-4GK527E:~ » curl -v -X POST \
  http://192.168.1.124:8123/api/services/light/turn_on \
  -H "Authorization: Bearer $HA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "entity_id": "light.ldvsmart_indflex2m",
    "hs_color": [0, 100],
    "brightness": 255
  }'
```

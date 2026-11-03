#!/usr/bin/env bash
# Paced verification: avoids rate-limiting the upstream while exercising every path.
cd /home/bagus/PROJECT/gemini-to-api || exit 1
B=http://localhost:4981/v1/chat/completions

ask() { # model extra_json label
  local m="$1" extra="$2" label="$3"
  local code c
  code=$(timeout 150 curl -s -o /tmp/vv.json -w "%{http_code}" -X POST "$B" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\"$extra,\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}]}" --max-time 140)
  c=$(python3 -c "import json;print(json.load(open('/tmp/vv.json')).get('choices',[{}])[0].get('message',{}).get('content','')[:20])" 2>/dev/null)
  printf "%-26s HTTP %s -> %s\n" "$label" "$code" "${c:-<none>}"
  sleep 15
}

echo "== non-stream, all models"
for m in gemini-flash-lite gemini-3.8-flash gemini-3.1-pro gemini-4-argon; do
  ask "$m" "" "$m"
done

echo "== reasoning effort (argon)"
ask gemini-4-argon ',"reasoning_effort":"medium"' "argon medium"

echo "== streaming"
for m in gemini-3.8-flash gemini-4-argon; do
  timeout 150 curl -sN -o /tmp/st.txt -w "stream $m HTTP %{http_code}\n" -X POST "$B" \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}]}" --max-time 140
  python3 - <<'EOF'
import json
t=[]
for line in open('/tmp/st.txt'):
    line=line.strip()
    if line.startswith('data:') and '[DONE]' not in line:
        try: d=json.loads(line[5:])
        except: continue
        for c in d.get('choices',[]):
            v=c.get('delta',{}).get('content') or ''
            if v: t.append(v)
print("  text =", repr(''.join(t))[:40])
EOF
  sleep 15
done

echo "== 2-turn recall (port #4)"
curl -s -X POST "$B" -H "Content-Type: application/json" \
  -d '{"model":"gemini-4-argon","messages":[{"role":"user","content":"Remember code KIWI-77. Reply OK."},{"role":"assistant","content":"OK"},{"role":"user","content":"What code? Answer code only."}]}' \
  --max-time 140 | python3 -c "import sys,json;d=json.load(sys.stdin);print('  recall =',repr(d['choices'][0]['message']['content']))"

apiVersion: v1
kind: ConfigMap
metadata:
  name: submariner-route-fix-scripts
  namespace: kube-system
data:
  sync-routes.sh: |
    #!/bin/sh
    set -eu
    SLEEP=${SYNC_INTERVAL:-10}

    ensure_route() {
      ip route replace "$1" dev vx-submariner scope link 2>/dev/null || true
    }

    delete_route() {
      ip route del "$1" dev vx-submariner 2>/dev/null || true
    }

    list_fdb_peers() {
      # list dst IPs from bridge fdb for vx-submariner
      bridge fdb show dev vx-submariner 2>/dev/null | awk '
        /dst/ {
          for (i=1;i<=NF;i++) {
            if ($i=="dst") {
              print $(i+1)
            }
          }
        }' | sort -u
    }

    list_existing_vx_routes() {
      # list routes that point at vx-submariner (handles IPv4 only)
      ip -4 route show | awk '/dev vx-submariner/ {print $1}' | sort -u
    }

    # main reconcile loop
    while true; do
      if ip link show vx-submariner >/dev/null 2>&1; then
        # gather desired and existing
        desired=$(list_fdb_peers || true)
        existing=$(list_existing_vx_routes || true)

        # add/ensure desired routes
        for ipdst in $desired; do
          # skip empty or non IPv4 addresses
          case "$ipdst" in
            ''|*:*) continue ;;
          esac
          ensure_route "$ipdst"
        done

        # remove stale routes that are not in desired
        for r in $existing; do
          found=0
          for d in $desired; do
            [ "$r" = "$d" ] && found=1 && break
          done
          [ "$found" -eq 0 ] && delete_route "$r"
        done
      fi

      sleep "$SLEEP"
    done
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: submariner-route-fix
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: submariner-route-fix
  template:
    metadata:
      labels:
        app: submariner-route-fix
    spec:
      hostNetwork: true
      # run on all nodes; adjust tolerations/nodeSelector if needed
      serviceAccountName: default
      containers:
      - name: route-fix
        image: alpine:3.18
        imagePullPolicy: IfNotPresent
        securityContext:
          privileged: false
          capabilities:
            add: ["NET_ADMIN", "NET_RAW"]
        command:
        - /bin/sh
        - -c
        - |
          set -eu
          apk add --no-cache iproute2 iproute2-doc bridge iputils >/dev/null 2>&1 || true
          cp /scripts/sync-routes.sh /tmp/sync-routes.sh
          chmod +x /tmp/sync-routes.sh
          /tmp/sync-routes.sh
        volumeMounts:
        - name: scripts
          mountPath: /scripts
          readOnly: true
      terminationGracePeriodSeconds: 10
      tolerations:
      - operator: "Exists"
      volumes:
      - name: scripts
        configMap:
          name: submariner-route-fix-scripts
          defaultMode: 0755
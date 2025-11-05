mapfile -t crds_to_delete < <(
  kubectl --context $CTX get crd -o jsonpath='{range .items[*]}{.metadata.name}{"|"}{.spec.group}{"\n"}{end}' 2>/dev/null | grep -E "istio" | cut -d'|' -f1
)

for crd in "${crds_to_delete[@]}"; do echo "$crd"; done
for crd in "${crds_to_delete[@]}"; do kubectl --context $CTX delete crds "$crd"; done
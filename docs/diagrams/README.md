# docs/diagrams

| File | Shows |
|---|---|
| `eks-to-be.xml` | The EKS target architecture. Rendered to `assets/eks-arc.png`, which the root `README.md` embeds. Design: `docs/design/eks-stateful-tier.md`. |

## It is hand-edited, and that is the point

Edges carry custom waypoints, captions are nudged, boxes are placed by eye. Edit the XML
in place — in the draw.io app, or with a targeted patch — and treat what it contains as
authoritative.

Two habits follow from that:

- **An edge with no `source`/`target` is usually deliberate.** draw.io's auto-routing
  sometimes sends a line through the middle of a box; detaching the edge and pinning it
  with absolute `sourcePoint`/`targetPoint` is the fix. Judge an edge by the rendered
  PNG, not by whether the XML says it is attached.
- **Re-read before writing.** The file is often open in the app while something else
  edits it, so coordinates read a minute ago may already be stale.

## Rendering

```bash
drawio -x -f png --scale 1.7 -o ../../assets/eks-arc.png eks-to-be.xml
```

Export only reads the XML; it never writes it. Re-export in the same commit as the edit,
or the published PNG and its source drift apart.

## Layers

The dashed boundary rectangles (`app_tier`, `infra_tier`, `order_stack`) sit on their own
`boundaries` layer. They are `fillColor=none`, so click the **dashed outline** to select
one — clicking inside grabs whatever is underneath. `Cmd+Shift+L` hides the whole layer
while rearranging icons.

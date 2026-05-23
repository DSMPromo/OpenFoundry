"""A dependency-free reference ModelAdapter — a tiny linear model.

It lets the adapter contract be exercised end to end (save, load,
predict, api) with no ML framework installed, and serves as a worked
example for authors writing their own adapters.
"""

from __future__ import annotations

import json
from pathlib import Path

from .adapter import Column, ModelAdapter, ModelApi, TabularShape, register


@register
class LinearModelAdapter(ModelAdapter):
    """A linear model: ``y = sum(weight_i * x_i) + bias`` over a row of
    floats. Serialised as a single JSON file — no dependencies."""

    adapter_id = "openfoundry.linear"
    _FILE = "linear_model.json"

    def __init__(self, weights: list[float], bias: float = 0.0) -> None:
        self.weights = [float(w) for w in weights]
        self.bias = float(bias)

    def save(self, path: Path) -> None:
        (path / self._FILE).write_text(
            json.dumps({"weights": self.weights, "bias": self.bias}),
            encoding="utf-8",
        )

    @classmethod
    def load(cls, path: Path) -> "LinearModelAdapter":
        params = json.loads((path / cls._FILE).read_text(encoding="utf-8"))
        return cls(weights=params["weights"], bias=params["bias"])

    def predict(self, data: list[list[float]]) -> list[float]:
        out: list[float] = []
        for row in data:
            if len(row) != len(self.weights):
                raise ValueError(
                    f"row has {len(row)} features, model expects {len(self.weights)}"
                )
            out.append(sum(w * x for w, x in zip(self.weights, row)) + self.bias)
        return out

    def api(self) -> ModelApi:
        features = [Column(name=f"f{i}", type="double") for i in range(len(self.weights))]
        return ModelApi(
            inputs=[TabularShape(name="features", columns=features)],
            outputs=[TabularShape(name="prediction", columns=[Column(name="y", type="double")])],
        )

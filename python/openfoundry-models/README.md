# openfoundry-models

The model adapter SDK for OpenFoundry — the open-source analogue of
`palantir_models`. A **model adapter** teaches the platform how to
serialise a trained model into a directory (the "model archive"), load
it back, declare its input/output schema, and run inference. Training,
the model registry, and live/batch deployments all consume models
through this one contract.

## The contract

Implement `ModelAdapter` — four operations:

| Method | Purpose |
|---|---|
| `save(path)` | serialise the model into a directory |
| `load(path)` *(classmethod)* | reconstruct the adapter from that directory |
| `predict(data)` | run inference |
| `api()` | declare the input/output schema (`ModelApi`) |

`save_model(adapter, path)` and `load_model(path)` wrap an archive with
metadata so it round-trips without the caller knowing the concrete
class. Register adapters with the `@register` decorator.

## Adapters included

- `LinearModelAdapter` — a dependency-free reference adapter (a tiny
  linear model) and worked example.
- `SklearnAdapter`, `XGBoostAdapter`, `PyTorchAdapter`,
  `HuggingFaceAdapter` — templates for the common frameworks. Each
  imports its library lazily, so `import openfoundry_models` works with
  none of them installed; install the matching extra to use one:

```sh
pip install "openfoundry-models[sklearn]"
```

## Example

```python
from openfoundry_models import LinearModelAdapter, save_model, load_model

model = LinearModelAdapter(weights=[2.0, -1.0], bias=0.5)
save_model(model, "/tmp/my-model")
reloaded = load_model("/tmp/my-model")
reloaded.predict([[1.0, 1.0]])  # -> [1.5]
```

## Develop

```sh
pip install -e ".[dev]"
pytest
```

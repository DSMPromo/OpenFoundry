"""Reusable ModelAdapter templates for the common ML frameworks.

Each adapter imports its framework lazily — inside the method that needs
it — so ``import openfoundry_models`` works with none of them installed.
Install the matching extra (e.g. ``pip install openfoundry-models[sklearn]``)
to use one.

``api()`` returns an empty :class:`ModelApi` by default; subclass and
override it to declare the model's real input/output schema.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from .adapter import ModelAdapter, ModelApi, register


@register
class SklearnAdapter(ModelAdapter):
    """Wraps any fitted scikit-learn estimator; persisted with joblib."""

    adapter_id = "openfoundry.sklearn"
    _FILE = "sklearn_model.joblib"

    def __init__(self, model: Any) -> None:
        self.model = model

    def save(self, path: Path) -> None:
        import joblib

        joblib.dump(self.model, path / self._FILE)

    @classmethod
    def load(cls, path: Path) -> "SklearnAdapter":
        import joblib

        return cls(joblib.load(path / cls._FILE))

    def predict(self, data: Any) -> Any:
        return self.model.predict(data)

    def api(self) -> ModelApi:
        return ModelApi()


@register
class XGBoostAdapter(ModelAdapter):
    """Wraps an xgboost.Booster; persisted via the native JSON format."""

    adapter_id = "openfoundry.xgboost"
    _FILE = "xgboost_model.json"

    def __init__(self, model: Any) -> None:
        self.model = model

    def save(self, path: Path) -> None:
        self.model.save_model(str(path / self._FILE))

    @classmethod
    def load(cls, path: Path) -> "XGBoostAdapter":
        import xgboost

        booster = xgboost.Booster()
        booster.load_model(str(path / cls._FILE))
        return cls(booster)

    def predict(self, data: Any) -> Any:
        import xgboost

        return self.model.predict(xgboost.DMatrix(data))

    def api(self) -> ModelApi:
        return ModelApi()


@register
class PyTorchAdapter(ModelAdapter):
    """Wraps a torch.nn.Module; persisted with torch.save."""

    adapter_id = "openfoundry.pytorch"
    _FILE = "pytorch_model.pt"

    def __init__(self, model: Any) -> None:
        self.model = model

    def save(self, path: Path) -> None:
        import torch

        torch.save(self.model, path / self._FILE)

    @classmethod
    def load(cls, path: Path) -> "PyTorchAdapter":
        import torch

        # weights_only=False: the template persists the whole module, so
        # the model class must be importable when loading.
        return cls(torch.load(path / cls._FILE, weights_only=False))

    def predict(self, data: Any) -> Any:
        self.model.eval()
        return self.model(data)

    def api(self) -> ModelApi:
        return ModelApi()


@register
class HuggingFaceAdapter(ModelAdapter):
    """Wraps a Hugging Face transformers model via save_pretrained."""

    adapter_id = "openfoundry.huggingface"

    def __init__(self, model: Any) -> None:
        self.model = model

    def save(self, path: Path) -> None:
        self.model.save_pretrained(str(path))

    @classmethod
    def load(cls, path: Path) -> "HuggingFaceAdapter":
        from transformers import AutoModel

        return cls(AutoModel.from_pretrained(str(path)))

    def predict(self, data: Any) -> Any:
        return self.model(**data)

    def api(self) -> ModelApi:
        return ModelApi()

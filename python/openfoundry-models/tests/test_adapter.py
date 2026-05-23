"""Contract tests for openfoundry-models.

They exercise the adapter contract end to end through the
dependency-free LinearModelAdapter, so no ML framework is needed.
"""

from pathlib import Path

import pytest

from openfoundry_models import (
    LinearModelAdapter,
    ModelAdapter,
    adapter_for,
    load_model,
    save_model,
)


def test_linear_adapter_predict():
    model = LinearModelAdapter(weights=[2.0, -1.0], bias=0.5)
    assert model.predict([[1.0, 1.0], [3.0, 0.0]]) == [1.5, 6.5]


def test_linear_adapter_rejects_wrong_feature_count():
    model = LinearModelAdapter(weights=[1.0, 1.0])
    with pytest.raises(ValueError):
        model.predict([[1.0]])


def test_save_load_round_trip(tmp_path: Path):
    original = LinearModelAdapter(weights=[1.5, 2.5], bias=-1.0)
    save_model(original, tmp_path)
    assert (tmp_path / "openfoundry_model.json").exists()

    loaded = load_model(tmp_path)
    assert isinstance(loaded, LinearModelAdapter)
    rows = [[1.0, 1.0], [2.0, -2.0]]
    assert loaded.predict(rows) == original.predict(rows)


def test_api_declares_input_and_output_schema():
    api = LinearModelAdapter(weights=[1.0, 1.0, 1.0]).api()
    assert [c.name for c in api.inputs[0].columns] == ["f0", "f1", "f2"]
    assert api.outputs[0].columns[0].name == "y"
    assert isinstance(api.to_dict(), dict)


def test_registry_resolves_every_bundled_adapter():
    for adapter_id in (
        "openfoundry.linear",
        "openfoundry.sklearn",
        "openfoundry.xgboost",
        "openfoundry.pytorch",
        "openfoundry.huggingface",
    ):
        assert issubclass(adapter_for(adapter_id), ModelAdapter)


def test_unknown_adapter_id_raises():
    with pytest.raises(KeyError):
        adapter_for("openfoundry.nonexistent")

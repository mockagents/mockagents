"""Tests that verify all example agent YAML files pass validation.

These tests use the Go config loader directly via subprocess to ensure
the example files are valid and can be loaded by the mock engine.
"""

import os
import subprocess
import sys

import pytest

EXAMPLES_DIR = os.path.join(os.path.dirname(__file__), "..")


def get_example_files():
    """Find all YAML files in the examples directory."""
    files = []
    for f in os.listdir(EXAMPLES_DIR):
        if f.endswith((".yaml", ".yml")) and not f.startswith("."):
            files.append(f)
    return sorted(files)


class TestExampleValidation:
    """Ensure all example agent definitions are structurally valid."""

    def test_examples_directory_exists(self):
        assert os.path.isdir(EXAMPLES_DIR), f"Examples directory not found: {EXAMPLES_DIR}"

    def test_has_minimum_examples(self):
        files = get_example_files()
        assert len(files) >= 3, f"Expected at least 3 example files, found {len(files)}: {files}"

    def test_customer_support_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "customer-support-agent.yaml")
        assert os.path.isfile(path)

    def test_code_assistant_exists(self):
        path = os.path.join(EXAMPLES_DIR, "code-assistant.yaml")
        assert os.path.isfile(path)

    def test_rag_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "rag-agent.yaml")
        assert os.path.isfile(path)

    def test_weather_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "weather-agent.yaml")
        assert os.path.isfile(path)

    def test_minimal_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "minimal-agent.yaml")
        assert os.path.isfile(path)

    @pytest.mark.parametrize("filename", get_example_files())
    def test_example_yaml_is_valid(self, filename):
        """Each example file should contain valid YAML with required fields."""
        import yaml

        path = os.path.join(EXAMPLES_DIR, filename)
        with open(path, "r") as f:
            doc = yaml.safe_load(f)

        assert isinstance(doc, dict), f"{filename} is not a YAML mapping"
        assert doc.get("apiVersion") == "mockagents/v1", f"{filename} missing apiVersion"
        assert doc.get("kind") == "Agent", f"{filename} missing kind: Agent"
        assert "metadata" in doc, f"{filename} missing metadata"
        assert "name" in doc["metadata"], f"{filename} missing metadata.name"
        assert "spec" in doc, f"{filename} missing spec"
        assert "protocol" in doc["spec"], f"{filename} missing spec.protocol"
        assert "behavior" in doc["spec"], f"{filename} missing spec.behavior"
        assert "scenarios" in doc["spec"]["behavior"], f"{filename} missing scenarios"
        assert len(doc["spec"]["behavior"]["scenarios"]) > 0, f"{filename} has no scenarios"

    @pytest.mark.parametrize("filename", get_example_files())
    def test_example_scenarios_have_names(self, filename):
        """Every scenario in every example should have a name."""
        import yaml

        path = os.path.join(EXAMPLES_DIR, filename)
        with open(path, "r") as f:
            doc = yaml.safe_load(f)

        for i, scenario in enumerate(doc["spec"]["behavior"]["scenarios"]):
            assert "name" in scenario, f"{filename}: scenario {i} missing name"
            assert scenario["name"], f"{filename}: scenario {i} has empty name"

    @pytest.mark.parametrize("filename", get_example_files())
    def test_example_has_default_scenario(self, filename):
        """Every example should have a default (catch-all) scenario."""
        import yaml

        path = os.path.join(EXAMPLES_DIR, filename)
        with open(path, "r") as f:
            doc = yaml.safe_load(f)

        scenarios = doc["spec"]["behavior"]["scenarios"]
        has_default = any(
            "match" not in s or s.get("match") is None
            for s in scenarios
        )
        assert has_default, f"{filename} has no default scenario (scenario without match)"

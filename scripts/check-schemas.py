"""Development-only contract verification; requires jsonschema==4.25.1."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile

from jsonschema import Draft202012Validator, ValidationError

root = Path(__file__).resolve().parents[1]
binary = Path(sys.argv[1]).resolve()
schemas = {
    name: json.loads((root / "docs" / f"{name}.schema.json").read_text(encoding="utf-8-sig"))
    for name in ("policy", "report")
}
for schema in schemas.values():
    Draft202012Validator.check_schema(schema)
policy_validator = Draft202012Validator(schemas["policy"])
report_validator = Draft202012Validator(schemas["report"])

def run(*args, input=None):
    result = subprocess.run([str(binary), *args], input=input, capture_output=True)
    return result.returncode, json.loads(result.stdout)

code, default = run("config", "init")
assert code == 0
policy_validator.validate(default)
policy_validator.validate(json.loads((root / "privsan.example.json").read_text(encoding="utf-8-sig")))
for options in ([], ["--content"], ["--content", "--hide-paths"]):
    code, report = run("scan", "--stdin", "--json", *options, input=b"alice@example.com 13800138000")
    assert code == 0 and report["complete"]
    report_validator.validate(report)
code, report = run("scan", "--stdin", "--json", "--content", "--max-file", "1", input=b"private")
assert code == 1 and not report["complete"]
report_validator.validate(report)
code, replacement_report = run("replace", "--find", "old", "--with", "new", "--stdin", "--dry-run", "--json", "--content", input=b"old text")
assert code == 0 and replacement_report["complete"]
assert replacement_report["policy_id"] == "replace-v1" and replacement_report["files"][0]["content"] == "new text"
report_validator.validate(replacement_report)
code, replacement_report = run("replace", "--find", "x", "--with", "long", "--stdin", "--dry-run", "--json", "--content", "--max-file", "2", input=b"x")
assert code == 1 and not replacement_report["complete"]
report_validator.validate(replacement_report)
report["files"] = [{"size": 1, "findings": [], "content": "must not exist"}]
try:
    report_validator.validate(report)
    raise AssertionError("Incomplete-content schema guard failed")
except ValidationError:
    pass

with tempfile.TemporaryDirectory() as temporary:
    base = Path(temporary)
    source = base / "source"
    source.mkdir()
    (source / "a.txt").write_bytes(b"alice@example.com")
    code, report = run("redact", "--in-place", "--backup-dir", str(base / "backup"), "--json", str(source))
    assert code == 0 and report["complete"]
    report_validator.validate(report)
    code, report = run("restore", "--from", report["operation"]["run_dir"], "--root", str(source), "--json")
    assert code == 0 and report["complete"]
    report_validator.validate(report)
print("PASS: policy/report JSON Schemas, CLI output contracts, failed-content prohibition, restore")

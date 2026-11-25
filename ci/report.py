"""Write JUnit for an external test command, or append a JUnit job summary."""
import argparse
import os
import subprocess
import time
import xml.etree.ElementTree as ET
from pathlib import Path


def report(args):
    started = time.monotonic()
    result = subprocess.run(args.command, check=False)
    elapsed = f"{time.monotonic() - started:.3f}"
    suite = ET.Element("testsuite", name=args.name, tests="1",
                       failures=str(int(result.returncode != 0)), time=elapsed)
    case = ET.SubElement(suite, "testcase", classname="integration",
                         name=args.name, time=elapsed)
    if result.returncode:
        ET.SubElement(case, "failure", message=f"exit {result.returncode}").text = (
            "See the test step log for full diagnostics."
        )
    ET.ElementTree(suite).write(args.xml, encoding="utf-8", xml_declaration=True)
    return result.returncode


def summary(xml):
    cases = list(ET.parse(xml).iter("testcase"))
    failed = [case for case in cases if case.find("failure") is not None
              or case.find("error") is not None]
    skipped = sum(case.find("skipped") is not None for case in cases)
    lines = [f"**{len(cases)} tests** · {len(failed)} failed · {skipped} skipped"]
    for case in failed:
        lines.append(f"- Failed: `{case.get('classname')}.{case.get('name')}`")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf8") as output:
        print("\n\n".join(lines), file=output)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("xml", type=Path)
    parser.add_argument("--summary", action="store_true")
    parser.add_argument("--name", default="integration")
    options, command = parser.parse_known_args()
    options.command = command[1:] if command[:1] == ["--"] else command
    if options.summary:
        summary(options.xml)
    else:
        if not options.command:
            parser.error("provide a command after --")
        raise SystemExit(report(options))

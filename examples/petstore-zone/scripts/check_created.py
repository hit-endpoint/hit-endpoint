"""Example 'after' hook. Runs with the Result and the variable Context.

Raise AssertionError to fail the request; call ctx.set(name, value) to capture.
"""


def after(result, ctx):
    assert result.json["age"] >= 0, "age must not be negative"
    ctx.set("last_created_name", result.json["name"])

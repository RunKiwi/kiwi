import pathlib

from setuptools import find_packages, setup

# PyPI renders long_description as the project page. Without it the page is
# blank, which costs both readers and the inbound link the page would carry.
HERE = pathlib.Path(__file__).parent
LONG_DESCRIPTION = (HERE / "README.md").read_text(encoding="utf-8")

setup(
    name="kiwi-sdk",
    # 0.x while the surface is still three methods. 1.0.0 is a semver promise
    # of API stability that this client is not ready to make.
    version="0.1.0",
    description="Python client for Kiwi — coding agents that run in infrastructure you control.",
    long_description=LONG_DESCRIPTION,
    long_description_content_type="text/markdown",
    url="https://runkiwi.dev",
    project_urls={
        "Documentation": "https://docs.runkiwi.dev",
        "Source": "https://github.com/RunKiwi/kiwi",
        "Issues": "https://github.com/RunKiwi/kiwi/issues",
        "Dashboard": "https://app.runkiwi.dev",
    },
    author="RunKiwi",
    license="MIT",
    packages=find_packages(exclude=["test_kiwi", "tests", "tests.*"]),
    install_requires=["requests"],
    python_requires=">=3.9",
    keywords=["kiwi", "runkiwi", "coding-agent", "ai-agent", "byoc", "automation"],
    classifiers=[
        "Development Status :: 4 - Beta",
        "Intended Audience :: Developers",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
        "Programming Language :: Python :: 3.12",
        "Topic :: Software Development :: Code Generators",
        "Topic :: Software Development :: Quality Assurance",
    ],
)

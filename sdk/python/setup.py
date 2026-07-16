from setuptools import setup, find_packages

setup(
    name="kiwi-sdk",
    version="1.0.0",
    packages=find_packages(),
    install_requires=["requests"],
    description="Kiwi BYOC SDK for Python",
)

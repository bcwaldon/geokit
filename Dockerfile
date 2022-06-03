FROM python:3.8

RUN apt update && apt install -y gdal-bin libgdal-dev
RUN pip install gdal==3.2.2

FROM ubuntu:jammy

RUN apt update && apt install -y gnupg wget python3 libx11-dev libxi-dev libxext-dev make g++ mesa-common-dev nodejs npm pkg-config

# needed to correctly install dependencies via yarn later
ENV CXXFLAGS="-include /usr/include/c++/11/limits"

# expected by yarn
RUN ln -s /usr/bin/python3 /usr/bin/python

ADD yarn-pubkey.gpg /tmp
RUN apt-key add /tmp/yarn-pubkey.gpg

RUN echo "deb https://dl.yarnpkg.com/debian/ stable main" | tee /etc/apt/sources.list.d/yarn.list
RUN apt update && apt install -y yarn

WORKDIR /opt

RUN wget https://github.com/keplergl/kepler.gl/archive/refs/tags/v2.5.5.tar.gz
RUN tar -xzvf v2.5.5.tar.gz

WORKDIR /opt/kepler.gl-2.5.5/
RUN yarn

WORKDIR /opt/kepler.gl-2.5.5/examples/demo-app/
RUN npm install

CMD npm start -- --host 0.0.0.0
